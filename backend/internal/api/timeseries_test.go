package api

import (
	"encoding/json"
	"math"
	"testing"
)

func TestTimeSeriesJSON(t *testing.T) {
	s := TimeSeries{
		Name:       "m<&>",
		Labels:     map[string]string{"service": "svc", "type": "input"},
		DataPoints: [][2]float64{{1762800000000, 1.5}, {1762800060000, math.NaN()}, {0, 1e-7}, {1, 1e21}, {2, math.Inf(1)}, {3, -0.25}},
	}
	got, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	// Numbers as encoding/json writes them, NaN and infinities as null
	type plain struct {
		Name       string            `json:"name"`
		Labels     map[string]string `json:"labels,omitempty"`
		DataPoints [][2]*float64     `json:"datapoints"`
	}
	f := func(v float64) *float64 { return &v }
	want, _ := json.Marshal(plain{s.Name, s.Labels, [][2]*float64{
		{f(1762800000000), f(1.5)}, {f(1762800060000), nil}, {f(0), f(1e-7)}, {f(1), f(1e21)}, {f(2), nil}, {f(3), f(-0.25)},
	}})
	if string(got) != string(want) {
		t.Errorf("JSON =\n%s\nwant\n%s", got, want)
	}

	var back TimeSeries
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != s.Name || back.Labels["type"] != "input" || len(back.DataPoints) != 6 ||
		back.DataPoints[0] != s.DataPoints[0] || !math.IsNaN(back.DataPoints[1][1]) || back.DataPoints[5][1] != -0.25 {
		t.Errorf("decoded %+v", back)
	}

	// No points and no labels
	got, _ = json.Marshal(TimeSeries{Name: "x"})
	if string(got) != `{"name":"x","datapoints":[]}` {
		t.Errorf("empty series = %s", got)
	}
}
