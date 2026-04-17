// Package httpregister exposes a minimal HTTP transport for agent registration
// and listing (POST /v1/register, GET /v1/agents) against a master.Facade.
// TLS is left to callers; tests use net/http/httptest.
package httpregister
