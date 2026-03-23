package domain

type Role string

const (
	RoleAdmin Role = "ADMIN"
	RoleUser  Role = "USER"
)

type Actor struct {
	UserID int64
	Role   Role
}

type User struct {
	ID           int64
	Login        string
	PasswordHash string
	Role         Role
}
