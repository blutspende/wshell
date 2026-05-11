package wshell

const (
	ContextKey_UserID        = "userid"
	ContextKey_UserEmail     = "email"
	ContextKey_GivenName     = "given_name"
	ContextKey_FamilyName    = "family_name"
	ContextKey_EmailVerified = "email_verified"
	ContextKey_Roles         = "roles"
	ContextKey_SourceIP      = "source_ip"
	ContextKey_Correlation   = "correlation_id"
)

// -- Errors provided through the API
type APIDescriptor struct {
	SpecPath string
	Handler  interface{}
}
