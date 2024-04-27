package models

type Config struct {
	StatsServer       string            `json:"statsServer"`
	EVSEConfiguration EVSEConfiguration `json:"evseConfiguration"`
}

type EVSEConfiguration struct {
	Enabled          bool   `json:"enabled"`
	Address          string `json:"address"`
	EnableControl    bool   `json:"enableControl"`
	MaxChargeCurrent int    `json:"maxChargeCurrent"`
	MinCurrentBuffer int    `json:"minCurrentBuffer"`
}
