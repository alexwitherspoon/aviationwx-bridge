// WeatherLink Live Local API simulator for local bridge testing.
// Spec: https://weatherlink.github.io/weatherlink-live-local-api/
package main

import (
	"encoding/json"
	"flag"
	"log"
	"math"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

// Base fixture matches internal/station/testdata/davis_current_conditions.json
// (doc-shaped structures; realistic ISS humidity unlike the public doc sample).
const baseFixture = `{
  "data": {
    "did": "001D0A700002",
    "ts": 1531754005,
    "conditions": [
      {
        "lsid": 48308,
        "data_structure_type": 1,
        "txid": 1,
        "temp": 62.7,
        "hum": 55.0,
        "dew_point": 45.0,
        "wind_speed_last": 10,
        "wind_dir_last": 270,
        "wind_speed_hi_last_10_min": 15,
        "rain_size": 1,
        "rainfall_daily": 3,
        "rx_state": 0,
        "trans_battery_flag": 0
      },
      {
        "lsid": 48307,
        "data_structure_type": 4,
        "temp_in": 78.0,
        "hum_in": 41.1
      },
      {
        "lsid": 48306,
        "data_structure_type": 3,
        "bar_sea_level": 30.008,
        "bar_absolute": 30.008
      }
    ]
  },
  "error": null
}`

type envelope struct {
	Data  map[string]json.RawMessage `json:"data"`
	Error *string                    `json:"error"`
}

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	var hits atomic.Uint64
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/current_conditions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		n := hits.Add(1)
		body, err := buildResponse(n)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("wll-simulator listening on %s (GET /v1/current_conditions)", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Printf("listen: %v", err)
		os.Exit(1)
	}
}

func buildResponse(hit uint64) ([]byte, error) {
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(baseFixture), &env); err != nil {
		return nil, err
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(env["data"], &data); err != nil {
		return nil, err
	}

	ts, err := json.Marshal(time.Now().UTC().Unix())
	if err != nil {
		return nil, err
	}
	data["ts"] = ts

	var conditions []map[string]interface{}
	if err := json.Unmarshal(data["conditions"], &conditions); err != nil {
		return nil, err
	}
	for _, c := range conditions {
		dst, _ := c["data_structure_type"].(float64)
		if int(dst) != 1 {
			continue
		}
		// Light mutation so successive polls / posts are visibly different.
		phase := float64(hit)
		c["wind_speed_last"] = 8 + math.Mod(phase, 7)
		c["wind_dir_last"] = math.Mod(270+phase*15, 360)
		c["temp"] = 62.0 + math.Mod(phase*0.3, 3)
	}
	condRaw, err := json.Marshal(conditions)
	if err != nil {
		return nil, err
	}
	data["conditions"] = condRaw

	dataRaw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	env["data"] = dataRaw
	return json.Marshal(env)
}
