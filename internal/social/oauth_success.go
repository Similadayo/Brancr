package social

import (
	"html/template"
	"net/http"
	"strings"
)

// OAuthSuccessHandler handles OAuth success/error page rendering
type OAuthSuccessHandler struct{}

// NewOAuthSuccessHandler creates a new OAuth success handler
func NewOAuthSuccessHandler() *OAuthSuccessHandler {
	return &OAuthSuccessHandler{}
}

// HandleSuccess renders the OAuth success/error page
func (h *OAuthSuccessHandler) HandleSuccess(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	status := query.Get("status")
	platforms := query.Get("platforms")
	reason := query.Get("reason")
	warnings := query.Get("warnings")

	var title, message, icon string
	var isSuccess bool

	if status == "success" {
		isSuccess = true
		title = "Connection Successful!"
		icon = "✅"
		if platforms != "" {
			platformList := strings.Split(platforms, ",")
			platformNames := make([]string, len(platformList))
			for i, p := range platformList {
				platformNames[i] = strings.Title(p)
			}
			message = "Your " + strings.Join(platformNames, ", ") + " account" + func() string {
				if len(platformNames) > 1 {
					return "s have"
				}
				return " has"
			}() + " been connected successfully."
		} else {
			message = "Your account has been connected successfully."
		}
		if warnings != "" && warnings != "0" {
			message += " Some platforms may have warnings - check your dashboard for details."
		}
	} else {
		isSuccess = false
		title = "Connection Failed"
		icon = "❌"
		if reason != "" {
			message = reason
		} else {
			message = "There was an error connecting your account. Please try again."
		}
	}

	data := map[string]interface{}{
		"Title":     title,
		"Message":   message,
		"Icon":      icon,
		"IsSuccess": isSuccess,
		"Platforms": platforms,
	}

	tmpl := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}} - Brancr</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .container {
            background: white;
            border-radius: 16px;
            box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
            max-width: 500px;
            width: 100%;
            padding: 48px 32px;
            text-align: center;
            animation: fadeIn 0.3s ease-out;
        }
        @keyframes fadeIn {
            from {
                opacity: 0;
                transform: translateY(20px);
            }
            to {
                opacity: 1;
                transform: translateY(0);
            }
        }
        .icon {
            font-size: 64px;
            margin-bottom: 24px;
            animation: scaleIn 0.5s ease-out;
        }
        @keyframes scaleIn {
            from {
                transform: scale(0);
                opacity: 0;
            }
            to {
                transform: scale(1);
                opacity: 1;
            }
        }
        h1 {
            font-size: 28px;
            font-weight: 700;
            color: #1a202c;
            margin-bottom: 16px;
        }
        .message {
            font-size: 16px;
            color: #4a5568;
            line-height: 1.6;
            margin-bottom: 32px;
        }
        .success {
            color: #059669;
        }
        .error {
            color: #dc2626;
        }
        .button {
            display: inline-block;
            padding: 12px 32px;
            background: #667eea;
            color: white;
            text-decoration: none;
            border-radius: 8px;
            font-weight: 600;
            transition: all 0.2s;
            margin: 4px;
            border: none;
            cursor: pointer;
            font-size: 16px;
        }
        .button:hover {
            background: #5568d3;
            transform: translateY(-2px);
            box-shadow: 0 4px 12px rgba(102, 126, 234, 0.4);
        }
        .button-secondary {
            background: #e2e8f0;
            color: #4a5568;
        }
        .button-secondary:hover {
            background: #cbd5e0;
        }
        .close-hint {
            font-size: 14px;
            color: #718096;
            margin-top: 24px;
        }
        .auto-close-notice {
            font-size: 12px;
            color: #a0aec0;
            margin-top: 16px;
            font-style: italic;
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">{{.Icon}}</div>
        <h1>{{.Title}}</h1>
        <p class="message {{if .IsSuccess}}success{{else}}error{{end}}">{{.Message}}</p>
        <div>
            <button onclick="closeWindow()" class="button">Close Window</button>
            {{if .IsSuccess}}
            <a href="/admin/social" class="button button-secondary" target="_blank">View Accounts</a>
            {{end}}
        </div>
        <p class="close-hint">You can safely close this window</p>
        <p class="auto-close-notice" id="countdown"></p>
    </div>
    <script>
        // Check if opened in popup (has window.opener)
        var isPopup = window.opener !== null;
        var countdownSeconds = 3;
        
        function closeWindow() {
            if (isPopup) {
                window.close();
            } else {
                // If not in popup, redirect to dashboard or show message
                window.location.href = '/admin';
            }
        }
        
        // Auto-close after countdown if opened in popup
        if (isPopup) {
            var countdownEl = document.getElementById('countdown');
            var interval = setInterval(function() {
                if (countdownSeconds > 0) {
                    countdownEl.textContent = 'This window will close automatically in ' + countdownSeconds + ' second' + (countdownSeconds > 1 ? 's' : '') + '...';
                    countdownSeconds--;
                } else {
                    clearInterval(interval);
                    countdownEl.textContent = 'Closing...';
                    window.close();
                }
            }, 1000);
        } else {
            // If not in popup, hide countdown
            document.getElementById('countdown').style.display = 'none';
        }
        
        // Also try to notify opener if it exists (for Telegram bot flow)
        if (window.opener) {
            try {
                // Try to post message to opener (if it's expecting it)
                window.opener.postMessage({type: 'oauth_success', status: '{{.IsSuccess}}', platforms: '{{.Platforms}}'}, '*');
            } catch (e) {
                // Ignore if cross-origin or not supported
            }
        }
    </script>
</body>
</html>`

	t, err := template.New("oauth_success").Parse(tmpl)
	if err != nil {
		http.Error(w, "Failed to render page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	t.Execute(w, data)
}

