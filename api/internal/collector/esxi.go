package collector

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	"net"
	"net/url"
	"signalatlas/api/internal/domain"
	"strings"
	"time"
)

func collectESXi(ctx context.Context, p domain.Profile, password string) (map[string]any, error) {
	u := &url.URL{Scheme: "https", Host: p.Host, Path: "/sdk"}
	sc := soap.NewClient(u, false)
	if p.CertificateSHA256 != "" {
		cfg, err := pinnedTLSConfig(p.CertificateSHA256)
		if err != nil {
			return nil, err
		}
		sc.DefaultTransport().TLSClientConfig = cfg
	}
	c, err := vim25.NewClient(ctx, sc)
	if err != nil {
		return nil, errors.New("ESXi API连接或证书验证失败，请核对地址与证书SHA256指纹")
	}
	sm := session.NewManager(c)
	if err = sm.Login(ctx, url.UserPassword(p.Username, password)); err != nil {
		return nil, errors.New("ESXi登录失败，请检查账号与密码")
	}
	defer func() {
		x, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		_ = sm.Logout(x)
	}()
	v, err := view.NewManager(c).CreateContainerView(ctx, c.ServiceContent.RootFolder, []string{"HostSystem"}, true)
	if err != nil {
		return nil, errors.New("无法读取ESXi主机视图")
	}
	defer v.Destroy(ctx)
	var hosts []mo.HostSystem
	if err = v.Retrieve(ctx, []string{"HostSystem"}, []string{"name", "runtime", "config.network", "vm"}, &hosts); err != nil {
		return nil, errors.New("读取ESXi网络属性失败")
	}
	pnics := []map[string]any{}
	vmks := []map[string]any{}
	pgs := []map[string]any{}
	switches := []map[string]any{}
	vms := []map[string]any{}
	self := []domain.Device{}
	for _, h := range hosts {
		if h.Config == nil || h.Config.Network == nil {
			continue
		}
		networkInfo := h.Config.Network
		for _, n := range networkInfo.Pnic {
			row := map[string]any{"device": n.Device, "mac": n.Mac, "driver": n.Driver}
			if n.LinkSpeed != nil {
				row["link_mbps"] = n.LinkSpeed.SpeedMb
				row["full_duplex"] = n.LinkSpeed.Duplex
			}
			pnics = append(pnics, row)
		}
		for _, n := range networkInfo.Vnic {
			row := map[string]any{"device": n.Device, "mac": n.Spec.Mac, "portgroup": n.Portgroup}
			if n.Spec.Ip != nil {
				row["ip"] = n.Spec.Ip.IpAddress
				row["dhcp"] = n.Spec.Ip.Dhcp
				if n.Spec.Ip.IpAddress == p.Host {
					if mac, err := net.ParseMAC(n.Spec.Mac); err == nil && len(mac) == 6 {
						self = append(self, domain.Device{MAC: strings.ToLower(mac.String()), IP: p.Host, Name: p.Name, Online: true, SeenVia: "ESXi API实时管理接口"})
					}
				}
			}
			vmks = append(vmks, row)
		}
		for _, n := range networkInfo.Portgroup {
			pgs = append(pgs, map[string]any{"name": n.Spec.Name, "vlan": n.Spec.VlanId, "vswitch": n.Spec.VswitchName})
		}
		for _, n := range networkInfo.Vswitch {
			switches = append(switches, map[string]any{"name": n.Name, "pnics": n.Pnic})
		}
		if len(h.Vm) > 0 {
			var vm []mo.VirtualMachine
			if err = property.DefaultCollector(c).Retrieve(ctx, h.Vm, []string{"name", "runtime.powerState", "config.hardware.device"}, &vm); err != nil {
				return nil, errors.New("读取虚拟机网络信息失败")
			}
			for _, m := range vm {
				cards := []map[string]any{}
				pci := []string{}
				if m.Config != nil {
					for _, d := range m.Config.Hardware.Device {
						if n, ok := d.(types.BaseVirtualEthernetCard); ok {
							card := n.GetVirtualEthernetCard()
							cards = append(cards, map[string]any{"mac": card.MacAddress, "label": card.DeviceInfo.GetDescription().Label})
						}
						if n, ok := d.(*types.VirtualPCIPassthrough); ok {
							if b, ok := n.Backing.(*types.VirtualPCIPassthroughDeviceBackingInfo); ok {
								pci = append(pci, b.Id)
							}
						}
					}
				}
				vms = append(vms, map[string]any{"name": m.Name, "power_state": m.Runtime.PowerState, "network_cards": cards, "pci_passthrough": pci})
			}
		}
	}
	return map[string]any{"model": c.ServiceContent.About.Name + " " + c.ServiceContent.About.Version, "firmware": "build " + c.ServiceContent.About.Build, "pnics": pnics, "vmkernel_interfaces": vmks, "portgroups": pgs, "vswitches": switches, "vms": vms, "devices": self, "stations": []domain.Station{}, "radios": []domain.Radio{}}, nil
}

func pinnedTLSConfig(fingerprint string) (*tls.Config, error) {
	expected, err := hex.DecodeString(strings.ReplaceAll(fingerprint, ":", ""))
	if err != nil || len(expected) != sha256.Size {
		return nil, errors.New("ESXi证书SHA256指纹无效")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Identity is the explicitly approved SHA256 leaf certificate, rather
		// than public CA/hostname trust. Every TLS handshake must match it.
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("ESXi未提供服务器证书")
			}
			cert := cs.PeerCertificates[0]
			sum := sha256.Sum256(cert.Raw)
			if subtle.ConstantTimeCompare(sum[:], expected) != 1 {
				return errors.New("ESXi服务器证书指纹不匹配")
			}
			if now := time.Now(); now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
				return errors.New("ESXi服务器证书不在有效期内")
			}
			return nil
		},
	}, nil
}
