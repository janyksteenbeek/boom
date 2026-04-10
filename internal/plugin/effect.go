package plugin

// Param describes a single tunable parameter of an effect.
type Param struct {
	Name    string  `json:"name"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Default float64 `json:"default"`
	Value   float64 `json:"value"`
}

// Effect is a pluggable audio effect that processes sample buffers.
type Effect interface {
	Name() string
	Process(samples [][2]float32) [][2]float32
	SetParam(name string, value float64)
	Params() []Param
}
