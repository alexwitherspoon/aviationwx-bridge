package time

import (
	"fmt"
	"time"

	"github.com/beevik/ntp"
)

// queryNTP queries an NTP server and returns the time offset
func (th *TimeHealth) queryNTP(server string) (time.Duration, error) {
	// Use default timeout if not configured
	timeout := 5 * time.Second

	// Query NTP server
	response, err := ntp.QueryWithOptions(server, ntp.QueryOptions{
		Timeout: timeout,
	})
	if err != nil {
		return 0, fmt.Errorf("NTP query failed: %w", err)
	}

	// Return clock offset (difference between local and NTP time)
	return response.ClockOffset, nil
}
