package ctxkeys

import "context"

type contextKey string

const (
	UserID   contextKey = "userID"
	UserRole contextKey = "userRole"
)

func GetUserID(ctx context.Context) string {
	id, _ := ctx.Value(UserID).(string)
	return id
}

func GetUserRole(ctx context.Context) string {
	role, _ := ctx.Value(UserRole).(string)
	return role
}
