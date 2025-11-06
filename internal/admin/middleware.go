package admin

import (
	"fmt"
	"net/http"
	"runtime/debug"

	"go.uber.org/zap"
)

func AdminPanicMiddleware(ctx *AdminContext) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := string(debug.Stack())
					WriteAdminLog(ctx, "ERROR", "panic", fmt.Sprint(rec), stack)
					ctx.Logger.Error("panic recovered", zap.Any("panic", rec))
					http.Error(w, "internal error", 500)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
