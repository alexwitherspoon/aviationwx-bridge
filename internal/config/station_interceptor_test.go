package config

import "testing"

func TestValidateHTTPInterceptorStation(t *testing.T) {
	st := Station{
		ID:         "station-wu",
		Name:       "WU",
		Type:       StationTypeHTTPInterceptor,
		ListenAddr: "0.0.0.0:8090",
		ListenPath: "/weatherstation/updateweatherstation.php",
		Dialect:    HTTPInterceptorDialectWunderground,
	}
	if err := ValidateStation(st); err != nil {
		t.Fatal(err)
	}

	badPath := st
	badPath.ListenPath = "weatherstation/update"
	if err := ValidateStation(badPath); err == nil {
		t.Fatal("expected listen_path without leading / to fail")
	}

	badDialect := st
	badDialect.Dialect = "ecowitt"
	if err := ValidateStation(badDialect); err == nil {
		t.Fatal("expected unsupported dialect to fail")
	}

	badAddr := st
	badAddr.ListenAddr = "8090"
	if err := ValidateStation(badAddr); err == nil {
		t.Fatal("expected listen_addr without host to fail")
	}

	nonNumeric := st
	nonNumeric.ListenAddr = "0.0.0.0:abc"
	if err := ValidateStation(nonNumeric); err == nil {
		t.Fatal("expected non-numeric port to fail")
	}

	zeroPort := st
	zeroPort.ListenAddr = "0.0.0.0:0"
	if err := ValidateStation(zeroPort); err == nil {
		t.Fatal("expected port 0 to fail")
	}
}

func TestNormalizeHTTPInterceptorDefaults(t *testing.T) {
	st := Station{Type: StationTypeHTTPInterceptor}
	NormalizeStationDefaults(&st)
	if st.ListenAddr != DefaultHTTPInterceptorListenAddr {
		t.Fatalf("listen_addr = %q", st.ListenAddr)
	}
	if st.ListenPath != DefaultHTTPInterceptorListenPath {
		t.Fatalf("listen_path = %q", st.ListenPath)
	}
	if st.Dialect != HTTPInterceptorDialectWunderground {
		t.Fatalf("dialect = %q", st.Dialect)
	}

	padded := Station{
		Type:       StationTypeHTTPInterceptor,
		ListenAddr: " 0.0.0.0:8090 ",
		ListenPath: " /weatherstation/updateweatherstation.php ",
		Dialect:    " wunderground ",
	}
	NormalizeStationDefaults(&padded)
	if padded.ListenAddr != "0.0.0.0:8090" || padded.ListenPath != "/weatherstation/updateweatherstation.php" || padded.Dialect != "wunderground" {
		t.Fatalf("expected trimmed fields, got %+v", padded)
	}
}
