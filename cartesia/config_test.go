package cartesia

import (
	"encoding/json"
	"testing"
)

func TestBufferDelayConfig(t *testing.T) {
	for _, test := range []struct {
		raw     string
		want    int
		invalid bool
	}{
		{raw: `{}`, want: 3000},
		{raw: `{"max_buffer_delay_ms":0}`, want: 0},
		{raw: `{"max_buffer_delay_ms":5000}`, want: 5000},
		{raw: `{"max_buffer_delay_ms":-1}`, invalid: true},
		{raw: `{"max_buffer_delay_ms":5001}`, invalid: true},
		{raw: `{"max_buffer_delay_ms":0.5}`, invalid: true},
	} {
		t.Run(test.raw, func(t *testing.T) {
			cfg, err := decodeConfig(json.RawMessage(test.raw))
			if test.invalid {
				if err == nil {
					t.Fatal("expected invalid delay error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.MaxBufferDelayMS == nil || *cfg.MaxBufferDelayMS != test.want {
				t.Fatalf("unexpected delay: %+v", cfg)
			}
		})
	}
}
