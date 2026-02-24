package social

import "fmt"

// GetCurrentAccountMetrics returns the list of currently allowed account-level metrics
// based on 2025 API deprecations. This helper helps ensure you're using only valid metrics.
func GetCurrentAccountMetrics(loginMethod string) ([]string, error) {
	// Valid account-level metrics (same for both login methods)
	// NOTE: "impressions" is NOT valid for account insights (only for media insights)
	return []string{
		"reach",
		"follower_count",
		"website_clicks",
		"profile_views",
		"online_followers",
		"accounts_engaged",
		"total_interactions",
		"likes",
		"comments",
		"shares",
		"saves",
		"replies",
	}, nil
}

// GetInvalidAccountMetrics returns a list of metrics that are NOT valid for account insights
// These should NOT be used in account-level insights API calls
func GetInvalidAccountMetrics() []string {
	return []string{
		"impressions", // Only valid for media insights, not account insights
	}
}

// IsMetricInvalidForAccount checks if a metric is invalid for account insights
func IsMetricInvalidForAccount(metric string) bool {
	invalid := GetInvalidAccountMetrics()
	for _, d := range invalid {
		if d == metric {
			return true
		}
	}
	return false
}

// ValidateMetricsAgainstCurrentAPI validates metrics against current (2025) API requirements
// Returns error if any metric is deprecated or invalid
func ValidateMetricsAgainstCurrentAPI(metrics []string, loginMethod string) error {
	allowedMetrics, err := GetCurrentAccountMetrics(loginMethod)
	if err != nil {
		return err
	}

	allowedMap := make(map[string]bool)
	for _, m := range allowedMetrics {
		allowedMap[m] = true
	}

	for _, metric := range metrics {
		if IsMetricInvalidForAccount(metric) {
			return fmt.Errorf("metric '%s' is not valid for account insights (only valid for media insights)", metric)
		}
		if !allowedMap[metric] {
			return fmt.Errorf("metric '%s' is not valid for %s accounts", metric, loginMethod)
		}
	}

	return nil
}
