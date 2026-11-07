package collector

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

const opticalFixture = "optical temp=9906\r\noptical SupplyVoltage=33047\r\noptical TXBiasCurrent=5209\r\noptical TXPower=16176\r\noptical RXPower=100\r\n"
const opticalIdentityFixture = "VendorName is ZTE_XGPON1ASY\nVendorPN is GN28L97\nVendorRev is 1.0\nVendorSN is must-not-export\nDateCde is 20241219\nDiagnosticMonitoringType is 0x68\n"

func opticalInput() opticalReadings {
	return opticalReadings{parameters: opticalFixture, identity: opticalIdentityFixture, vendor: "optical pn = 22", link: `<DM name="DownCurrBitRate" val="10000000"/><DM name="UpCurrBitRate" val="2500000"/>`, fec: "fec state is 1", business: "TxPower=31653\r\nRxPower=125\r\nTemperature=9905\r\nSupplyVottage=33040\r\n", display: "PONRPower=-19  PONTPower=4  Temperature=38"}
}
func num(v float64) *float64 { return &v }
func TestOpticalBusinessValuesMatchManagementLayer(t *testing.T) {
	o := parseOpticalModule(opticalInput(), time.Now())
	for name, pair := range map[string][2]*float64{"temperature": {o.TemperatureC, num(38)}, "voltage": {o.VoltageV, num(3.304)}, "bias": {o.BiasMA, num(10.418)}, "rx": {o.RXDBM, num(10 * math.Log10(.0125))}, "tx": {o.TXDBM, num(10 * math.Log10(3.1653))}, "down": {o.DownMbps, num(10000)}, "up": {o.UpMbps, num(2500)}} {
		if pair[0] == nil || math.Abs(*pair[0]-*pair[1]) > 1e-8 {
			t.Fatalf("incorrect %s", name)
		}
	}
	if o.Status != "ok" || o.MeasurementSource != "zte_qoe_v2" || o.ModuleType != "XG_BOSA" || o.FECEnabled == nil || !*o.FECEnabled {
		t.Fatal("metadata missing")
	}
	b, _ := json.Marshal(o)
	if strings.Contains(string(b), "must-not-export") || strings.Contains(string(b), "VendorSN") {
		t.Fatal("serial leaked")
	}
}
func TestNoFallbackToWrongDiagnosticPowerOrHardcodedCompensation(t *testing.T) {
	i := opticalInput()
	i.business = ""
	o := parseOpticalModule(i, time.Now())
	if o.RXDBM != nil || o.TXDBM != nil || o.VoltageV != nil {
		t.Fatal("diagnostic values substituted for unavailable business readings")
	}
	i = opticalInput()
	i.business = "RxPower=200\nTxPower=10000\nSupplyVottage=33000\n"
	o = parseOpticalModule(i, time.Now())
	if o.TXDBM == nil || *o.TXDBM != 0 || math.Abs(*o.RXDBM-10*math.Log10(.02)) > 1e-8 {
		t.Fatal("fixed compensation applied")
	}
}
func TestOpticsMissingZeroInvalidAndUnverifiedCalibration(t *testing.T) {
	i := opticalInput()
	i.business = "RxPower=0\nSupplyVottage=999999"
	i.display = "Temperature=-1"
	o := parseOpticalModule(i, time.Now())
	if o.TemperatureC == nil || *o.TemperatureC != -1 || o.RXDBM != nil || o.RXMW == nil || *o.RXMW != 0 || o.VoltageV != nil || o.TXDBM != nil {
		t.Fatal("missing or zero handling incorrect")
	}
	for _, identity := range []string{"", strings.ReplaceAll(opticalIdentityFixture, "0x68", "0x50")} {
		i = opticalInput()
		i.identity = identity
		o = parseOpticalModule(i, time.Now())
		if o.TemperatureC != nil || o.TXDBM != nil {
			t.Fatal("unverified format was guessed")
		}
	}
}
