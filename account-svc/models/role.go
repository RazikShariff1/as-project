package models

import "time"

// Role is a named set of permissions. A role with AllAccess may call every endpoint.
type Role struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	AllAccess   bool      `json:"allAccess"`
	EndpointIDs []int64   `json:"endpointIds"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// RoleRequest creates or replaces a role. EndpointIDs replaces the endpoints the role may call.
type RoleRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	AllAccess   bool    `json:"allAccess"`
	EndpointIDs []int64 `json:"endpointIds"`
}

// RoleList is a page of roles with the total match count.
type RoleList struct {
	Roles  []Role `json:"roles"`
	Count  int    `json:"count"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}
