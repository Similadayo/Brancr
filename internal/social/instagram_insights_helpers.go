package social

import "fmt"

// GetCurrentAccountMetrics returns the list of currently allowed account-level metrics
// based on 2025 API deprecations. This helper helps ensure you're using only valid metrics.
func GetCurrentAccountMetrics(loginMethod string) ([]string, error) {
	if loginMethod == "instagram_login" {
		// Instagram Login API - very limited, only 2 metrics after 2025 deprecations
		return []string{"impressions", "reach"}, nil
	}
	
	// Facebook Login API - full business metrics (excluding deprecated ones)
	return []string{
		"reach",
		"follower_count",
		"online_followers",
		"accounts_engaged",
		"total_interactions",
		"likes",
		"comments",
		"shares",
		"saves",
		"replies",
		"impressions",
	}, nil
}

// GetDeprecatedAccountMetrics returns a list of metrics that were deprecated in 2025
// These should NOT be used in new API calls
func GetDeprecatedAccountMetrics() []string {
	return []string{
		"profile_views",
		"website_clicks",
		"email_contacts",
		"get_directions_clicks",
		"phone_call_clicks",
		"text_message_clicks",
	}
}

// IsMetricDeprecated checks if a metric was deprecated in 2025
func IsMetricDeprecated(metric string) bool {
	deprecated := GetDeprecatedAccountMetrics()
	for _, d := range deprecated {
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
		if IsMetricDeprecated(metric) {
			return fmt.Errorf("metric '%s' was deprecated in 2025 and is no longer supported", metric)
		}
		if !allowedMap[metric] {
			return fmt.Errorf("metric '%s' is not valid for %s accounts", metric, loginMethod)
		}
	}
	
	return nil
}

