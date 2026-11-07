package collector

import (
	"context"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"signalatlas/api/internal/domain"
)

// Fixed, read-only commands documented by this device's opticaltst/gpontest help.
const opticalParametersCommand = "opticaltst -getpara"
const opticalIdentityCommand = "opticaltst -gettransceiverinfo"
const opticalVendorCommand = "opticaltst -getvendor"
const opticalLinkCommand = "sendcmd 1 DB p PhyLinkInfo"
const opticalFECCommand = "gpontest -gfecstate"
const opticalBusinessCommand = "sendcmd 1 optical_mgr getqoepara"
const opticalBusinessDisplayCommand = "sendcmd 1 optical_mgr getopticalinfo"

type opticalReadings struct{ parameters, identity, vendor, link, fec, business, display string }

func opticalBusinessRaw(text, key string) *float64 {
	n := value(`(?m)^\s*`+regexp.QuoteMeta(key)+`\s*=\s*(-?\d+)\s*$`, opticalText(text))
	if n == nil || *n < 0 || *n > 65535 {
		return nil
	}
	return n
}

func opticalText(s string) string { return strings.ReplaceAll(s, "\r", "") }
func opticalIdentity(text, key string) string {
	value := match(`(?m)^\s*`+regexp.QuoteMeta(key)+`\s+is\s+([^\n]{1,80})\s*$`, text)
	return strings.TrimSpace(value)
}
func opticalRaw(text, key string) *float64 {
	n := value(`(?m)^\s*optical\s+`+regexp.QuoteMeta(key)+`\s*=\s*(-?\d+)\s*$`, text)
	if n == nil || *n < 0 || *n > 65535 {
		return nil
	}
	return n
}
func opticalScale(n *float64, scale float64) *float64 {
	if n == nil {
		return nil
	}
	v := *n * scale
	return &v
}
func opticalDBM(raw *float64) *float64 {
	if raw == nil || *raw <= 0 {
		return nil
	}
	v := 10 * math.Log10(*raw/10000)
	return &v
}
func parseOpticalModule(input opticalReadings, at time.Time) domain.OpticalModule {
	parameters, identity, vendor, link, fec := opticalText(input.parameters), opticalText(input.identity), input.vendor, input.link, input.fec
	result := domain.OpticalModule{Status: "unavailable", Source: "厂家 optical_mgr 业务接口（功率/温度/电压）；opticaltst（偏置/模块资料）", MeasurementSource: "zte_qoe_v2", CollectedAt: at, Calibration: "unknown"}
	result.Manufacturer = opticalIdentity(identity, "VendorName")
	result.PartNumber = opticalIdentity(identity, "VendorPN")
	result.Revision = opticalIdentity(identity, "VendorRev")
	result.DateCode = opticalIdentity(identity, "DateCde")
	// VendorSN is deliberately excluded: registration/identity secrets are not telemetry.
	result.MonitoringType = opticalIdentity(identity, "DiagnosticMonitoringType")
	if flags, err := strconv.ParseUint(result.MonitoringType, 0, 8); err == nil {
		supported := flags&0x40 != 0
		result.DDMSupported = &supported
		if flags&0x20 != 0 && flags&0x10 == 0 {
			result.Calibration = "internal"
		} else if flags&0x10 != 0 {
			result.Calibration = "external"
		}
	}
	// The optical manager's business readings match the management page.
	// Do not substitute opticaltst power: on this device that yields a different
	// measurement layer (~1dB RX / ~3dB TX difference). No fixed offsets are used.
	if result.Calibration == "internal" && result.DDMSupported != nil && *result.DDMSupported {
		temp := value(`(?i)\bTemperature=(-?\d+)\b`, input.display)
		if temp != nil && *temp >= -128 && *temp <= 127 {
			result.TemperatureC = temp
		}
		result.VoltageV = opticalScale(opticalBusinessRaw(input.business, "SupplyVottage"), .0001)
		result.BiasMA = opticalScale(opticalRaw(parameters, "TXBiasCurrent"), .002)
		rx, tx := opticalBusinessRaw(input.business, "RxPower"), opticalBusinessRaw(input.business, "TxPower")
		result.RXMW = opticalScale(rx, .0001)
		result.TXMW = opticalScale(tx, .0001)
		result.RXDBM = opticalDBM(rx)
		result.TXDBM = opticalDBM(tx)
	}
	if match(`(?m)^\s*optical pn\s*=\s*(\d+)`, opticalText(vendor)) == "22" {
		result.ModuleType = "XG_BOSA"
	}
	result.DownMbps = opticalScale(value(`<DM\s+name="DownCurrBitRate"\s+val="(\d+)"`, link), .001)
	result.UpMbps = opticalScale(value(`<DM\s+name="UpCurrBitRate"\s+val="(\d+)"`, link), .001)
	if state := match(`(?i)fec state is\s*([01])\b`, fec); state != "" {
		enabled := state == "1"
		result.FECEnabled = &enabled
	}
	count := 0
	for _, v := range []*float64{result.TemperatureC, result.VoltageV, result.BiasMA, result.RXMW, result.TXMW} {
		if v != nil {
			count++
		}
	}
	if count == 5 {
		result.Status = "ok"
	} else if count > 0 || result.PartNumber != "" || result.DownMbps != nil {
		result.Status = "partial"
	}
	if result.Calibration != "internal" {
		result.Note = "未确认内部校准格式，光学数值未换算；不支持的字段留空。"
	} else if count < 5 {
		result.Note = "部分光模块字段未返回或超出格式范围；缺失值不补零。"
	}
	return result
}

func collectOpticalModule(ctx context.Context, t *telnet) domain.OpticalModule {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	var input opticalReadings
	commands := []struct {
		command string
		target  *string
	}{
		{opticalBusinessCommand, &input.business}, {opticalBusinessDisplayCommand, &input.display},
		{opticalParametersCommand, &input.parameters}, {opticalIdentityCommand, &input.identity},
		{opticalVendorCommand, &input.vendor}, {opticalLinkCommand, &input.link}, {opticalFECCommand, &input.fec},
	}
	failed := false
	for _, step := range commands {
		raw, err := t.run(ctx, step.command)
		if err != nil {
			failed = true
			break
		}
		*step.target = raw
	}
	result := parseOpticalModule(input, time.Now())
	if failed {
		result.Note = "光模块补充读取未完成；已返回的字段保留，未读取的字段留空。"
	}
	return result
}
