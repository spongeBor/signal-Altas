package wanmeter

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

const (
	oidIfName     = "1.3.6.1.2.1.31.1.1.1.1"
	oidHCInOctets = "1.3.6.1.2.1.31.1.1.1.6"
	oidHCOutOctet = "1.3.6.1.2.1.31.1.1.1.10"
)

// SNMPSource reads counters from the router's snmpd. The connection is opened
// lazily so an unreachable agent does not stop the API from starting.
type SNMPSource struct {
	host      string
	community string
	timeout   time.Duration
	conn      *gosnmp.GoSNMP
}

func NewSNMP(host, community string, timeout time.Duration) *SNMPSource {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &SNMPSource{host: host, community: community, timeout: timeout}
}

func (s *SNMPSource) connect() error {
	conn := &gosnmp.GoSNMP{
		Target:    s.host,
		Port:      161,
		Community: s.community,
		Version:   gosnmp.Version2c,
		Timeout:   s.timeout,
		Retries:   0,
		MaxOids:   2,
	}
	if err := conn.Connect(); err != nil {
		return err
	}
	s.conn = conn
	return nil
}

func (s *SNMPSource) Close() {
	if s.conn != nil && s.conn.Conn != nil {
		_ = s.conn.Conn.Close()
	}
	s.conn = nil
}

// Resolve turns an interface name into its SNMP index by walking the ifName
// table, because pppoe-wan keeps a different index in the 64-bit counter table
// than in the legacy interface table.
func (s *SNMPSource) Resolve(name string) (int, error) {
	if s.conn == nil {
		if err := s.connect(); err != nil {
			return 0, err
		}
	}
	index := 0
	err := s.conn.Walk(oidIfName, func(pdu gosnmp.SnmpPDU) error {
		if text, ok := octets(pdu.Value); ok && text == name {
			if n, err := oidIndex(pdu.Name); err == nil {
				index = n
			}
		}
		return nil
	})
	if err != nil {
		s.Close()
		return 0, err
	}
	if index == 0 {
		return 0, fmt.Errorf("SNMP ifName 里找不到接口 %s", name)
	}
	return index, nil
}

func (s *SNMPSource) Counters(index int) (uint64, uint64, error) {
	if s.conn == nil {
		if err := s.connect(); err != nil {
			return 0, 0, err
		}
	}
	packet, err := s.conn.Get([]string{
		fmt.Sprintf("%s.%d", oidHCInOctets, index),
		fmt.Sprintf("%s.%d", oidHCOutOctet, index),
	})
	if err != nil {
		s.Close()
		return 0, 0, err
	}
	if packet == nil || len(packet.Variables) != 2 {
		return 0, 0, errors.New("SNMP 返回的计数器数量不符")
	}
	in, ok := counter(packet.Variables[0].Value)
	if !ok {
		return 0, 0, errors.New("SNMP 接收计数器类型无效")
	}
	out, ok := counter(packet.Variables[1].Value)
	if !ok {
		return 0, 0, errors.New("SNMP 发送计数器类型无效")
	}
	return in, out, nil
}

func octets(v any) (string, bool) {
	switch x := v.(type) {
	case []byte:
		return string(x), true
	case string:
		return x, true
	}
	return "", false
}

func counter(v any) (uint64, bool) {
	switch x := v.(type) {
	case uint64:
		return x, true
	case uint32:
		return uint64(x), true
	case int:
		if x >= 0 {
			return uint64(x), true
		}
	}
	return 0, false
}

func oidIndex(oid string) (int, error) {
	i := strings.LastIndexByte(oid, '.')
	if i < 0 || i == len(oid)-1 {
		return 0, fmt.Errorf("OID 没有索引：%s", oid)
	}
	return strconv.Atoi(oid[i+1:])
}
