package models

import "time"

// HTTP methods an endpoint can be registered for.
const (
	MethodGet    = "GET"
	MethodPost   = "POST"
	MethodPut    = "PUT"
	MethodPatch  = "PATCH"
	MethodDelete = "DELETE"
)

// ValidMethod reports whether m is a registrable HTTP method.
func ValidMethod(m string) bool {
	switch m {
	case MethodGet, MethodPost, MethodPut, MethodPatch, MethodDelete:
		return true
	default:
		return false
	}
}

// Endpoint is something a service exposes. Path may contain {name} segments that match any single path segment.
type Endpoint struct {
	ID          int64     `json:"id"`
	Service     string    `json:"service"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// EndpointRequest creates or replaces an endpoint.
type EndpointRequest struct {
	Service     string `json:"service"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

// EndpointList is a page of endpoints with the total match count.
type EndpointList struct {
	Endpoints []Endpoint `json:"endpoints"`
	Count     int        `json:"count"`
	Offset    int        `json:"offset"`
	Limit     int        `json:"limit"`
}
