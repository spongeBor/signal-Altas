package domain

import "time"

// OpticalModule contains only read-only diagnostics, never registration secrets.
type OpticalModule struct {
	MeasurementSource string    `json:"measurement_source"`
	Status            string    `json:"status"`
	Source            string    `json:"source"`
	CollectedAt       time.Time `json:"collected_at"`
	Note              string    `json:"note,omitempty"`
	TemperatureC      *float64  `json:"temperature_c"`
	VoltageV          *float64  `json:"voltage_v"`
	BiasMA            *float64  `json:"bias_ma"`
	RXDBM             *float64  `json:"rx_dbm"`
	TXDBM             *float64  `json:"tx_dbm"`
	RXMW              *float64  `json:"rx_mw"`
	TXMW              *float64  `json:"tx_mw"`
	Manufacturer      string    `json:"manufacturer,omitempty"`
	PartNumber        string    `json:"part_number,omitempty"`
	Revision          string    `json:"revision,omitempty"`
	DateCode          string    `json:"date_code,omitempty"`
	MonitoringType    string    `json:"monitoring_type,omitempty"`
	Calibration       string    `json:"calibration"`
	DDMSupported      *bool     `json:"ddm_supported"`
	ModuleType        string    `json:"module_type,omitempty"`
	DownMbps          *float64  `json:"down_mbps"`
	UpMbps            *float64  `json:"up_mbps"`
	FECEnabled        *bool     `json:"fec_enabled"`
}
