package authz

// IsCommunityAuthority reports whether an authority belongs to the community
// edition, which supports system administrators and tenant administrators.
func IsCommunityAuthority(authority string) bool {
	return authority == "SYS_ADMIN" || authority == "TENANT_ADMIN"
}
