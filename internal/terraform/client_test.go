package terraform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

const testCertificatePEM = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----"

func TestNewClientDefaultTimeoutSupportsLiveProvisioning(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:8080", "test-token", nil)
	if err != nil {
		t.Fatal(err)
	}
	if client.client.Timeout != defaultClientTimeout || client.client.Timeout < 5*time.Minute {
		t.Fatalf("expected provisioning-safe default timeout, got %s", client.client.Timeout)
	}

	custom := &http.Client{Timeout: 2 * time.Second}
	client, err = NewClient("http://127.0.0.1:8080", "test-token", custom)
	if err != nil {
		t.Fatal(err)
	}
	if client.client != custom {
		t.Fatalf("expected custom HTTP client to be preserved")
	}
}

func TestClientProjectLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing bearer auth: %q", r.Header.Get("Authorization"))
		}
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/projects":
			var got CreateProjectRequest
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Ref != "alpha" || got.Name != "Alpha" || got.ResourceTier != "small" {
				t.Fatalf("unexpected create payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"project_1","ref":"alpha","org_id":"org_1","name":"Alpha","status":"healthy","spec":{"resource_tier":"small"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha":
			_, _ = w.Write([]byte(`{"id":"project_1","ref":"alpha","org_id":"org_1","name":"Alpha","status":"healthy","spec":{"resource_tier":"small"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/", "test-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.CreateProject(context.Background(), "org_1", CreateProjectRequest{Ref: "alpha", Name: "Alpha", ResourceTier: "small"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if project.ID != "project_1" || project.Status != "healthy" || project.Spec.ResourceTier != "small" {
		t.Fatalf("unexpected project %#v", project)
	}
	if _, err := client.GetProject(context.Background(), "alpha"); err != nil {
		t.Fatalf("get project: %v", err)
	}
	if err := client.DeleteProject(context.Background(), "alpha"); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	expected := []string{
		"POST /v1/orgs/org_1/projects",
		"GET /v1/projects/alpha",
		"DELETE /v1/projects/alpha",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientPlatformSettingsRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/settings/defaults":
			_, _ = w.Write([]byte(`{"domain":"supadupa.test","stack_version":"latest","profile":"full","resource_tier":"custom","backup_schedule":"daily","smtp":{"enabled":false,"host":"","port":587,"sender_name":"","sender_email":"","username":"","password_handle":"","tls_mode":"starttls"},"updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/settings/defaults":
			var got PlatformDefaultsInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Domain != "supadupa.internal" || got.StackVersion != "2026.06" || got.Profile != "essential" || got.ResourceTier != "custom" || got.BackupSchedule != "hourly" {
				t.Fatalf("unexpected platform defaults payload %#v", got)
			}
			if !got.SMTP.Enabled || got.SMTP.Host != "smtp.example.com" || got.SMTP.Port != 2525 || got.SMTP.PasswordHandle != "secret://platform/smtp-password" || got.SMTP.TLSMode != "implicit" {
				t.Fatalf("unexpected platform smtp payload %#v", got.SMTP)
			}
			_, _ = w.Write([]byte(`{"domain":"supadupa.internal","stack_version":"2026.06","profile":"essential","resource_tier":"custom","backup_schedule":"hourly","smtp":{"enabled":true,"host":"smtp.example.com","port":2525,"sender_name":"supadupa","sender_email":"noreply@example.com","username":"apikey","password_handle":"secret://platform/smtp-password","tls_mode":"implicit"},"updated_at":"2026-06-05T12:01:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/settings/sso":
			_, _ = w.Write([]byte(`{"enabled":false,"provider":"saml","idp_entity_id":"","sso_url":"","certificate_pem":"","acs_url":"","metadata_url":"","email_domain":"","auto_provision":false,"default_role":"developer","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/settings/sso":
			var got PlatformSSOConfigInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if !got.Enabled || got.IDPEntityID != "https://idp.example.com/saml" || got.SSOURL != "https://idp.example.com/login" || got.Certificate != testCertificatePEM || got.ACSURL != "https://supadupa.example.com/v1/auth/sso/saml/callback" || got.MetadataURL != "https://idp.example.com/metadata" || got.EmailDomain != "example.com" || !got.AutoProvision || got.DefaultRole != "developer" {
				t.Fatalf("unexpected platform sso payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"enabled":true,"provider":"saml","idp_entity_id":"https://idp.example.com/saml","sso_url":"https://idp.example.com/login","certificate_pem":"********","acs_url":"https://supadupa.example.com/v1/auth/sso/saml/callback","metadata_url":"https://idp.example.com/metadata","email_domain":"example.com","auto_provision":true,"default_role":"developer","updated_at":"2026-06-05T12:01:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := client.GetPlatformDefaults(context.Background())
	if err != nil {
		t.Fatalf("get platform defaults: %v", err)
	}
	if defaults.Domain != "supadupa.test" || defaults.SMTP.Port != 587 || defaults.SMTP.TLSMode != "starttls" || defaults.UpdatedAt.IsZero() {
		t.Fatalf("unexpected platform defaults %#v", defaults)
	}
	defaults, err = client.UpdatePlatformDefaults(context.Background(), PlatformDefaultsInput{
		Domain:         "supadupa.internal",
		StackVersion:   "2026.06",
		Profile:        "essential",
		ResourceTier:   "custom",
		BackupSchedule: "hourly",
		SMTP: PlatformSMTP{
			Enabled:        true,
			Host:           "smtp.example.com",
			Port:           2525,
			SenderName:     "supadupa",
			SenderEmail:    "noreply@example.com",
			Username:       "apikey",
			PasswordHandle: "secret://platform/smtp-password",
			TLSMode:        "implicit",
		},
	})
	if err != nil {
		t.Fatalf("update platform defaults: %v", err)
	}
	if defaults.Profile != "essential" || defaults.ResourceTier != "custom" || !defaults.SMTP.Enabled || defaults.SMTP.Host != "smtp.example.com" {
		t.Fatalf("unexpected updated defaults %#v", defaults)
	}
	sso, err := client.GetPlatformSSOConfig(context.Background())
	if err != nil {
		t.Fatalf("get platform sso: %v", err)
	}
	if sso.Enabled || sso.Provider != "saml" || sso.DefaultRole != "developer" {
		t.Fatalf("unexpected platform sso %#v", sso)
	}
	sso, err = client.UpdatePlatformSSOConfig(context.Background(), PlatformSSOConfigInput{
		Enabled:       true,
		IDPEntityID:   "https://idp.example.com/saml",
		SSOURL:        "https://idp.example.com/login",
		Certificate:   testCertificatePEM,
		ACSURL:        "https://supadupa.example.com/v1/auth/sso/saml/callback",
		MetadataURL:   "https://idp.example.com/metadata",
		EmailDomain:   "example.com",
		AutoProvision: true,
		DefaultRole:   "developer",
	})
	if err != nil {
		t.Fatalf("update platform sso: %v", err)
	}
	if !sso.Enabled || sso.Certificate != "********" || sso.EmailDomain != "example.com" {
		t.Fatalf("unexpected updated sso %#v", sso)
	}

	expected := []string{
		"GET /v1/settings/defaults",
		"PUT /v1/settings/defaults",
		"GET /v1/settings/sso",
		"PUT /v1/settings/sso",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientHostLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/hosts":
			var got HostInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "east-1a" || got.Address != "10.0.0.12" || got.Capacity.CPU != 8 || got.Capacity.RAMMB != 32768 || got.Capacity.DiskGB != 500 || got.Capacity.Project != 10 {
				t.Fatalf("unexpected host payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"host_1","name":"east-1a","address":"10.0.0.12","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10},"used":{"cpu":0,"ram_mb":0,"disk_gb":0,"projects":0},"created_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/hosts/host_1":
			_, _ = w.Write([]byte(`{"id":"host_1","name":"east-1a","address":"10.0.0.12","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10},"used":{"cpu":1,"ram_mb":2048,"disk_gb":20,"projects":1},"created_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/hosts":
			_, _ = w.Write([]byte(`[{"id":"host_1","name":"east-1a","address":"10.0.0.12","capacity":{"cpu":8,"ram_mb":32768,"disk_gb":500,"projects":10},"used":{"cpu":1,"ram_mb":2048,"disk_gb":20,"projects":1},"created_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/hosts/host_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	host, err := client.CreateHost(context.Background(), HostInput{
		Name:    "east-1a",
		Address: "10.0.0.12",
		Capacity: HostCapacity{
			CPU:     8,
			RAMMB:   32768,
			DiskGB:  500,
			Project: 10,
		},
	})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	if host.ID != "host_1" || host.Capacity.Project != 10 {
		t.Fatalf("unexpected host %#v", host)
	}
	host, err = client.GetHost(context.Background(), "host_1")
	if err != nil {
		t.Fatalf("get host: %v", err)
	}
	if host.Used.Project != 1 {
		t.Fatalf("unexpected host usage %#v", host.Used)
	}
	hosts, err := client.ListHosts(context.Background())
	if err != nil {
		t.Fatalf("list hosts: %v", err)
	}
	if len(hosts) != 1 || hosts[0].ID != "host_1" {
		t.Fatalf("unexpected hosts %#v", hosts)
	}
	if err := client.DeleteHost(context.Background(), "host_1"); err != nil {
		t.Fatalf("delete host: %v", err)
	}

	expected := []string{
		"POST /v1/hosts",
		"GET /v1/hosts/host_1",
		"GET /v1/hosts",
		"DELETE /v1/hosts/host_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientOrgTeamAccessQuotaRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/quotas":
			_, _ = w.Write([]byte(`{"org_id":"org_1","max_projects":10,"max_cpu":32,"max_ram_mb":65536,"max_disk_gb":2048,"used":{"cpu":4,"ram_mb":8192,"disk_gb":200},"updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/orgs/org_1/quotas":
			var got OrgQuotaInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.MaxProjects != 20 || got.MaxCPU != 64 || got.MaxRAMMB != 131072 || got.MaxDiskGB != 4096 {
				t.Fatalf("unexpected quota payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"org_id":"org_1","max_projects":20,"max_cpu":64,"max_ram_mb":131072,"max_disk_gb":4096,"used":{"cpu":4,"ram_mb":8192,"disk_gb":200},"updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/members":
			_, _ = w.Write([]byte(`[{"user_id":"user_1","org_id":"org_1","email":"dev@example.com","role":"developer","created_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/members":
			var got OrgMemberInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Email != "dev@example.com" || got.Role != "admin" {
				t.Fatalf("unexpected member payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"user_id":"user_1","org_id":"org_1","email":"dev@example.com","role":"admin","created_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/orgs/org_1/members/dev@example.com":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/teams":
			_, _ = w.Write([]byte(`[{"id":"team_1","org_id":"org_1","name":"Platform","slug":"platform","created_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/teams":
			var got OrgTeamInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "Platform" || got.Slug != "platform" {
				t.Fatalf("unexpected team payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"team_1","org_id":"org_1","name":"Platform","slug":"platform","created_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/orgs/org_1/teams/platform":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1/teams/platform/members":
			_, _ = w.Write([]byte(`[{"team_id":"team_1","org_id":"org_1","team_slug":"platform","user_id":"user_1","email":"dev@example.com","created_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs/org_1/teams/platform/members":
			var got OrgTeamMemberInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Email != "dev@example.com" {
				t.Fatalf("unexpected team member payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"team_id":"team_1","org_id":"org_1","team_slug":"platform","user_id":"user_1","email":"dev@example.com","created_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/orgs/org_1/teams/platform/members/dev@example.com":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/access":
			_, _ = w.Write([]byte(`[{"id":"grant_1","project_ref":"alpha","org_id":"org_1","subject_type":"team","subject_id":"platform","subject_name":"Platform","role":"admin","created_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/access":
			var got ProjectAccessGrantInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.SubjectType != "team" || got.SubjectID != "platform" || got.Role != "admin" {
				t.Fatalf("unexpected project access payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"grant_1","project_ref":"alpha","org_id":"org_1","subject_type":"team","subject_id":"platform","subject_name":"Platform","role":"admin","created_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/access/team/platform":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	quota, err := client.GetOrgQuota(context.Background(), "org_1")
	if err != nil {
		t.Fatalf("get org quota: %v", err)
	}
	if quota.OrgID != "org_1" || quota.Used.CPU != 4 || quota.UpdatedAt.IsZero() {
		t.Fatalf("unexpected quota %#v", quota)
	}
	quota, err = client.UpdateOrgQuota(context.Background(), "org_1", OrgQuotaInput{MaxProjects: 20, MaxCPU: 64, MaxRAMMB: 131072, MaxDiskGB: 4096})
	if err != nil {
		t.Fatalf("update org quota: %v", err)
	}
	if quota.MaxDiskGB != 4096 {
		t.Fatalf("unexpected updated quota %#v", quota)
	}
	members, err := client.ListOrgMembers(context.Background(), "org_1")
	if err != nil {
		t.Fatalf("list org members: %v", err)
	}
	if len(members) != 1 || members[0].Email != "dev@example.com" {
		t.Fatalf("unexpected members %#v", members)
	}
	member, err := client.UpsertOrgMember(context.Background(), "org_1", OrgMemberInput{Email: "dev@example.com", Role: "admin"})
	if err != nil {
		t.Fatalf("upsert org member: %v", err)
	}
	if member.Role != "admin" || member.CreatedAt.IsZero() {
		t.Fatalf("unexpected member %#v", member)
	}
	if err := client.DeleteOrgMember(context.Background(), "org_1", "dev@example.com"); err != nil {
		t.Fatalf("delete org member: %v", err)
	}
	teams, err := client.ListOrgTeams(context.Background(), "org_1")
	if err != nil {
		t.Fatalf("list org teams: %v", err)
	}
	if len(teams) != 1 || teams[0].Slug != "platform" {
		t.Fatalf("unexpected teams %#v", teams)
	}
	team, err := client.CreateOrgTeam(context.Background(), "org_1", OrgTeamInput{Name: "Platform", Slug: "platform"})
	if err != nil {
		t.Fatalf("create org team: %v", err)
	}
	if team.ID != "team_1" || team.CreatedAt.IsZero() {
		t.Fatalf("unexpected team %#v", team)
	}
	if err := client.DeleteOrgTeam(context.Background(), "org_1", "platform"); err != nil {
		t.Fatalf("delete org team: %v", err)
	}
	teamMembers, err := client.ListOrgTeamMembers(context.Background(), "org_1", "platform")
	if err != nil {
		t.Fatalf("list org team members: %v", err)
	}
	if len(teamMembers) != 1 || teamMembers[0].TeamSlug != "platform" {
		t.Fatalf("unexpected team members %#v", teamMembers)
	}
	teamMember, err := client.UpsertOrgTeamMember(context.Background(), "org_1", "platform", OrgTeamMemberInput{Email: "dev@example.com"})
	if err != nil {
		t.Fatalf("upsert org team member: %v", err)
	}
	if teamMember.TeamID != "team_1" || teamMember.UserID != "user_1" {
		t.Fatalf("unexpected team member %#v", teamMember)
	}
	if err := client.DeleteOrgTeamMember(context.Background(), "org_1", "platform", "dev@example.com"); err != nil {
		t.Fatalf("delete org team member: %v", err)
	}
	grants, err := client.ListProjectAccess(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project access: %v", err)
	}
	if len(grants) != 1 || grants[0].SubjectName != "Platform" {
		t.Fatalf("unexpected project grants %#v", grants)
	}
	grant, err := client.UpsertProjectAccess(context.Background(), "alpha", ProjectAccessGrantInput{SubjectType: "team", SubjectID: "platform", Role: "admin"})
	if err != nil {
		t.Fatalf("upsert project access: %v", err)
	}
	if grant.ID != "grant_1" || grant.Role != "admin" || grant.CreatedAt.IsZero() {
		t.Fatalf("unexpected project grant %#v", grant)
	}
	if err := client.DeleteProjectAccess(context.Background(), "alpha", "team", "platform"); err != nil {
		t.Fatalf("delete project access: %v", err)
	}

	expected := []string{
		"GET /v1/orgs/org_1/quotas",
		"PUT /v1/orgs/org_1/quotas",
		"GET /v1/orgs/org_1/members",
		"POST /v1/orgs/org_1/members",
		"DELETE /v1/orgs/org_1/members/dev@example.com",
		"GET /v1/orgs/org_1/teams",
		"POST /v1/orgs/org_1/teams",
		"DELETE /v1/orgs/org_1/teams/platform",
		"GET /v1/orgs/org_1/teams/platform/members",
		"POST /v1/orgs/org_1/teams/platform/members",
		"DELETE /v1/orgs/org_1/teams/platform/members/dev@example.com",
		"GET /v1/projects/alpha/access",
		"PUT /v1/projects/alpha/access",
		"DELETE /v1/projects/alpha/access/team/platform",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectBackupAndPITRPolicyRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/backups/policy":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"schedule":"daily","kind":"logical","storage_target_id":"target_1","last_run_at":"2026-06-05T01:00:00Z","next_run_at":"2026-06-06T02:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/backups/policy":
			var got ProjectBackupPolicyInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if !got.Enabled || got.Schedule != "hourly" || got.Kind != "physical" || got.StorageTargetID != "target_2" {
				t.Fatalf("unexpected backup policy payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"schedule":"hourly","kind":"physical","storage_target_id":"target_2","next_run_at":"2026-06-05T13:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/pitr/policy":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":false,"archive_bucket":"","retention_days":7,"updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/pitr/policy":
			var got ProjectPITRPolicyInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if !got.Enabled || got.ArchiveBucket != "s3://supadupa-pitr/alpha" || got.RetentionDays != 14 {
				t.Fatalf("unexpected PITR policy payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"archive_bucket":"s3://supadupa-pitr/alpha","retention_days":14,"last_archive_at":"2026-06-05T11:59:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	backupPolicy, err := client.GetProjectBackupPolicy(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("get backup policy: %v", err)
	}
	if backupPolicy.ProjectRef != "alpha" || backupPolicy.Schedule != "daily" || backupPolicy.StorageTargetID != "target_1" || backupPolicy.LastRunAt == nil || backupPolicy.NextRunAt == nil {
		t.Fatalf("unexpected backup policy %#v", backupPolicy)
	}
	backupPolicy, err = client.UpdateProjectBackupPolicy(context.Background(), "alpha", ProjectBackupPolicyInput{Enabled: true, Schedule: "hourly", Kind: "physical", StorageTargetID: "target_2"})
	if err != nil {
		t.Fatalf("update backup policy: %v", err)
	}
	if backupPolicy.Schedule != "hourly" || backupPolicy.Kind != "physical" || backupPolicy.StorageTargetID != "target_2" || backupPolicy.NextRunAt == nil {
		t.Fatalf("unexpected updated backup policy %#v", backupPolicy)
	}
	pitrPolicy, err := client.GetProjectPITRPolicy(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("get PITR policy: %v", err)
	}
	if pitrPolicy.ProjectRef != "alpha" || pitrPolicy.Enabled || pitrPolicy.RetentionDays != 7 {
		t.Fatalf("unexpected PITR policy %#v", pitrPolicy)
	}
	pitrPolicy, err = client.UpdateProjectPITRPolicy(context.Background(), "alpha", ProjectPITRPolicyInput{Enabled: true, ArchiveBucket: "s3://supadupa-pitr/alpha", RetentionDays: 14})
	if err != nil {
		t.Fatalf("update PITR policy: %v", err)
	}
	if !pitrPolicy.Enabled || pitrPolicy.ArchiveBucket != "s3://supadupa-pitr/alpha" || pitrPolicy.LastArchiveAt == nil {
		t.Fatalf("unexpected updated PITR policy %#v", pitrPolicy)
	}

	expected := []string{
		"GET /v1/projects/alpha/backups/policy",
		"PUT /v1/projects/alpha/backups/policy",
		"GET /v1/projects/alpha/pitr/policy",
		"PUT /v1/projects/alpha/pitr/policy",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientBackupStorageTargetLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/backup-storage-targets":
			var got BackupStorageTargetInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "R2" || got.Endpoint != "https://account.r2.cloudflarestorage.com" || got.Bucket != "supadupa" || got.SecretAccessKey != "secret-key" || !got.Default {
				t.Fatalf("unexpected target create payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"target_1","name":"R2","type":"s3","endpoint":"https://account.r2.cloudflarestorage.com","region":"auto","bucket":"supadupa","prefix":"prod","access_key_id":"access-key","secret_configured":true,"force_path_style":true,"default":true,"durable_off_host":true,"recovery_ready":false,"readiness_status":"validation-pending","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/backup-storage-targets":
			_, _ = w.Write([]byte(`[{"id":"target_1","name":"R2","type":"s3","endpoint":"https://account.r2.cloudflarestorage.com","region":"auto","bucket":"supadupa","prefix":"prod","access_key_id":"access-key","secret_configured":true,"force_path_style":true,"default":true,"durable_off_host":true,"recovery_ready":true,"readiness_status":"off-host-ready","last_tested_at":"2026-06-05T12:30:00Z","last_test_status":"passed","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:30:00Z"}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/backup-storage-targets/target_1":
			var got BackupStorageTargetInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "R2" || got.Bucket != "supadupa-new" || got.SecretAccessKey != "" || !got.ForcePathStyle {
				t.Fatalf("unexpected target update payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"target_1","name":"R2","type":"s3","endpoint":"https://account.r2.cloudflarestorage.com","region":"auto","bucket":"supadupa-new","prefix":"prod","access_key_id":"access-key","secret_configured":true,"force_path_style":true,"default":true,"durable_off_host":true,"recovery_ready":true,"readiness_status":"off-host-ready","last_test_status":"passed","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:30:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/backup-storage-targets/target_1/test":
			_, _ = w.Write([]byte(`{"id":"target_1","name":"R2","type":"s3","endpoint":"https://account.r2.cloudflarestorage.com","region":"auto","bucket":"supadupa-new","secret_configured":true,"durable_off_host":true,"recovery_ready":true,"readiness_status":"off-host-ready","last_test_status":"passed","last_tested_at":"2026-06-05T12:45:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/backup-storage-targets/target_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	target, err := client.CreateBackupStorageTarget(context.Background(), BackupStorageTargetInput{Name: "R2", Type: "s3", Endpoint: "https://account.r2.cloudflarestorage.com", Region: "auto", Bucket: "supadupa", Prefix: "prod", AccessKeyID: "access-key", SecretAccessKey: "secret-key", ForcePathStyle: true, Default: true})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	if target.ID != "target_1" || !target.SecretConfigured || target.ReadinessStatus != "validation-pending" {
		t.Fatalf("unexpected created target %#v", target)
	}
	targets, err := client.ListBackupStorageTargets(context.Background())
	if err != nil {
		t.Fatalf("list targets: %v", err)
	}
	if len(targets) != 1 || !targets[0].RecoveryReady || targets[0].LastTestedAt == nil {
		t.Fatalf("unexpected targets %#v", targets)
	}
	target, err = client.UpdateBackupStorageTarget(context.Background(), "target_1", BackupStorageTargetInput{Name: "R2", Type: "s3", Endpoint: "https://account.r2.cloudflarestorage.com", Region: "auto", Bucket: "supadupa-new", Prefix: "prod", AccessKeyID: "access-key", ForcePathStyle: true, Default: true})
	if err != nil {
		t.Fatalf("update target: %v", err)
	}
	if target.Bucket != "supadupa-new" || !target.RecoveryReady {
		t.Fatalf("unexpected updated target %#v", target)
	}
	target, err = client.TestBackupStorageTarget(context.Background(), "target_1")
	if err != nil {
		t.Fatalf("test target: %v", err)
	}
	if target.LastTestStatus != "passed" || !target.RecoveryReady {
		t.Fatalf("unexpected tested target %#v", target)
	}
	if err := client.DeleteBackupStorageTarget(context.Background(), "target_1"); err != nil {
		t.Fatalf("delete target: %v", err)
	}
	expected := []string{
		"POST /v1/backup-storage-targets",
		"GET /v1/backup-storage-targets",
		"PUT /v1/backup-storage-targets/target_1",
		"POST /v1/backup-storage-targets/target_1/test",
		"DELETE /v1/backup-storage-targets/target_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestBackupStorageTargetResourceStateAndInput(t *testing.T) {
	secret := types.StringValue("secret-key")
	model := backupStorageTargetResourceModel{
		Name:            types.StringValue("R2"),
		Type:            types.StringValue("s3"),
		Endpoint:        types.StringValue("https://account.r2.cloudflarestorage.com"),
		Region:          types.StringValue("auto"),
		Bucket:          types.StringValue("supadupa"),
		Prefix:          types.StringValue("prod"),
		AccessKeyID:     types.StringValue("access-key"),
		SecretAccessKey: secret,
		ForcePathStyle:  types.BoolValue(true),
		Default:         types.BoolValue(true),
	}
	input := backupStorageTargetInputFromModel(model)
	if input.SecretAccessKey != "secret-key" || input.Bucket != "supadupa" || !input.ForcePathStyle || !input.Default {
		t.Fatalf("unexpected target input %#v", input)
	}
	setBackupStorageTargetState(&model, BackupStorageTarget{
		ID:               "target_1",
		Name:             "R2",
		Type:             "s3",
		Endpoint:         "https://account.r2.cloudflarestorage.com",
		Region:           "auto",
		Bucket:           "supadupa",
		Prefix:           "prod",
		AccessKeyID:      "access-key",
		SecretConfigured: true,
		ForcePathStyle:   true,
		Default:          true,
		DurableOffHost:   true,
		RecoveryReady:    true,
		ReadinessStatus:  "off-host-ready",
		LastTestStatus:   "passed",
	})
	if model.ID.ValueString() != "target_1" || !model.SecretConfigured.ValueBool() || !model.DurableOffHost.ValueBool() || !model.RecoveryReady.ValueBool() {
		t.Fatalf("unexpected target state %#v", model)
	}
	if model.SecretAccessKey.ValueString() != "secret-key" {
		t.Fatalf("secret access key should stay from config/state")
	}
}

func TestClientProjectBranchAndReplicaRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/branches":
			var got ProjectBranchInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Ref != "alpha-preview" || got.Name != "Alpha Preview" || got.TTLHours != 24 || got.WithData {
				t.Fatalf("unexpected branch payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"branch":{"id":"branch_1","source_project_ref":"alpha","project_ref":"alpha-preview","name":"Alpha Preview","with_data":false,"status":"healthy","created_at":"2026-06-05T12:00:00Z","expires_at":"2026-06-06T12:00:00Z"},"project":{"id":"project_preview","ref":"alpha-preview","org_id":"org_1","name":"Alpha Preview","status":"healthy","spec":{"resource_tier":"small"}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/branches":
			_, _ = w.Write([]byte(`[{"id":"branch_1","source_project_ref":"alpha","project_ref":"alpha-preview","name":"Alpha Preview","with_data":false,"status":"healthy","created_at":"2026-06-05T12:00:00Z","expires_at":"2026-06-06T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/branches/alpha-preview":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/replicas":
			var got ProjectReplicaInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "alpha-read-1" || got.HostID != "host-2" || got.Region != "us-east-2" || got.Tier != "medium" || got.ReadWeight != 75 || got.FailoverPriority != 1 {
				t.Fatalf("unexpected replica payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"replica_1","project_ref":"alpha","name":"alpha-read-1","host_id":"host-2","region":"us-east-2","tier":"medium","status":"healthy","role":"read","message":"replica provisioned","read_uri":"postgres://read.example.com/postgres","read_weight":75,"failover_priority":1,"replication_lag_bytes":1024,"replication_lag_seconds":2,"created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:01:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/replicas":
			_, _ = w.Write([]byte(`[{"id":"replica_1","project_ref":"alpha","name":"alpha-read-1","host_id":"host-2","region":"us-east-2","tier":"medium","status":"healthy","role":"read","read_uri":"postgres://read.example.com/postgres","read_weight":75,"failover_priority":1,"replication_lag_bytes":1024,"replication_lag_seconds":2,"created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:01:00Z"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/replicas/routing":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","primary_uri":"postgres://primary.example.com/postgres","read_strategy":"weighted-healthy","auto_failover":true,"healthy_read_targets":[{"replica_id":"replica_1","name":"alpha-read-1","uri":"postgres://read.example.com/postgres","region":"us-east-2","weight":75,"failover_priority":1,"replication_lag_bytes":1024,"replication_lag_seconds":2,"role":"read","status":"healthy"}],"all_targets":[{"replica_id":"replica_1","name":"alpha-read-1","uri":"postgres://read.example.com/postgres","region":"us-east-2","weight":75,"failover_priority":1,"replication_lag_bytes":1024,"replication_lag_seconds":2,"role":"read","status":"healthy"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/replicas/replica_1/promote":
			var got ProjectReplicaActionInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Reason != "planned failover" {
				t.Fatalf("unexpected promote payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"replica_1","project_ref":"alpha","name":"alpha-read-1","region":"us-east-2","tier":"medium","status":"healthy","role":"primary","message":"planned failover","read_uri":"postgres://read.example.com/postgres","read_weight":75,"failover_priority":1,"promoted_at":"2026-06-05T12:02:00Z","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:02:00Z"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/replicas/failover":
			var got ProjectReplicaActionInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Reason != "primary degraded" {
				t.Fatalf("unexpected failover payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"replica_1","project_ref":"alpha","name":"alpha-read-1","region":"us-east-2","tier":"medium","status":"healthy","role":"primary","message":"primary degraded","read_uri":"postgres://read.example.com/postgres","read_weight":75,"failover_priority":1,"promoted_at":"2026-06-05T12:03:00Z","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:03:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/replicas/replica_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	branch, project, err := client.CreateProjectBranch(context.Background(), "alpha", ProjectBranchInput{Ref: "alpha-preview", Name: "Alpha Preview", TTLHours: 24})
	if err != nil {
		t.Fatalf("create project branch: %v", err)
	}
	if branch.ID != "branch_1" || branch.ProjectRef != "alpha-preview" || branch.WithData || branch.ExpiresAt == nil || project.ID != "project_preview" {
		t.Fatalf("unexpected branch/project %#v %#v", branch, project)
	}
	branches, err := client.ListProjectBranches(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project branches: %v", err)
	}
	if len(branches) != 1 || branches[0].ProjectRef != "alpha-preview" {
		t.Fatalf("unexpected branches %#v", branches)
	}
	if err := client.DeleteProjectBranch(context.Background(), "alpha", "alpha-preview"); err != nil {
		t.Fatalf("delete project branch: %v", err)
	}
	replica, err := client.CreateProjectReplica(context.Background(), "alpha", ProjectReplicaInput{Name: "alpha-read-1", HostID: "host-2", Region: "us-east-2", Tier: "medium", ReadWeight: 75, FailoverPriority: 1})
	if err != nil {
		t.Fatalf("create project replica: %v", err)
	}
	if replica.ID != "replica_1" || replica.ReadWeight != 75 || replica.CreatedAt.IsZero() || replica.UpdatedAt.IsZero() {
		t.Fatalf("unexpected replica %#v", replica)
	}
	replicas, err := client.ListProjectReplicas(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project replicas: %v", err)
	}
	if len(replicas) != 1 || replicas[0].Name != "alpha-read-1" {
		t.Fatalf("unexpected replicas %#v", replicas)
	}
	routing, err := client.GetProjectReplicaRouting(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("get project replica routing: %v", err)
	}
	if routing.ReadStrategy != "weighted-healthy" || !routing.AutoFailover || len(routing.HealthyReadTargets) != 1 {
		t.Fatalf("unexpected routing %#v", routing)
	}
	promoted, err := client.PromoteProjectReplica(context.Background(), "alpha", "replica_1", "planned failover")
	if err != nil {
		t.Fatalf("promote project replica: %v", err)
	}
	if promoted.Role != "primary" || promoted.PromotedAt == nil {
		t.Fatalf("unexpected promoted replica %#v", promoted)
	}
	failedOver, err := client.FailoverProjectReplica(context.Background(), "alpha", "primary degraded")
	if err != nil {
		t.Fatalf("failover project replica: %v", err)
	}
	if failedOver.Role != "primary" || failedOver.Message != "primary degraded" {
		t.Fatalf("unexpected failover replica %#v", failedOver)
	}
	if err := client.DeleteProjectReplica(context.Background(), "alpha", "replica_1"); err != nil {
		t.Fatalf("delete project replica: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/branches",
		"GET /v1/projects/alpha/branches",
		"DELETE /v1/projects/alpha/branches/alpha-preview",
		"POST /v1/projects/alpha/replicas",
		"GET /v1/projects/alpha/replicas",
		"GET /v1/projects/alpha/replicas/routing",
		"POST /v1/projects/alpha/replicas/replica_1/promote",
		"POST /v1/projects/alpha/replicas/failover",
		"DELETE /v1/projects/alpha/replicas/replica_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectConfigLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/config/auth_providers":
			var got ProjectConfigInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Config["oauth_google_enabled"] != "true" || got.Config["oauth_google_client_secret_handle"] != "secret://projects/alpha/google" || got.Config["oauth_discord_client_secret_handle"] != "secret://projects/alpha/discord" || got.Config["oauth_oidc_client_secret_handle"] != "secret://projects/alpha/oidc" || got.Config["sms_messagebird_access_key_handle"] != "secret://projects/alpha/messagebird" {
				t.Fatalf("unexpected config payload %#v", got.Config)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","area":"auth_providers","config":{"oauth_google_enabled":"true","oauth_google_client_secret_handle":"secret://projects/alpha/google","oauth_discord_client_secret_handle":"secret://projects/alpha/discord","oauth_oidc_client_secret_handle":"secret://projects/alpha/oidc","sms_messagebird_access_key_handle":"secret://projects/alpha/messagebird","saml_enabled":"false"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/config/auth_providers":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","area":"auth_providers","config":{"oauth_google_enabled":"true","saml_enabled":"false"}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := client.UpdateProjectConfig(context.Background(), "alpha", "auth_providers", map[string]string{
		"oauth_google_enabled":               "true",
		"oauth_google_client_secret_handle":  "secret://projects/alpha/google",
		"oauth_discord_client_secret_handle": "secret://projects/alpha/discord",
		"oauth_oidc_client_secret_handle":    "secret://projects/alpha/oidc",
		"sms_messagebird_access_key_handle":  "secret://projects/alpha/messagebird",
	})
	if err != nil {
		t.Fatalf("update project config: %v", err)
	}
	if updated.ProjectRef != "alpha" || updated.Area != "auth_providers" || updated.Config["saml_enabled"] != "false" {
		t.Fatalf("unexpected updated config %#v", updated)
	}
	current, err := client.GetProjectConfig(context.Background(), "alpha", "auth_providers")
	if err != nil {
		t.Fatalf("get project config: %v", err)
	}
	if current.Config["oauth_google_enabled"] != "true" {
		t.Fatalf("unexpected current config %#v", current)
	}

	expected := []string{
		"PUT /v1/projects/alpha/config/auth_providers",
		"GET /v1/projects/alpha/config/auth_providers",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectDomainLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/domains":
			var got ProjectDomainInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.FQDN != "api.example.com" {
				t.Fatalf("unexpected domain payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","fqdn":"api.example.com","cert_status":"issued","cert_mode":"byo"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/domains":
			_, _ = w.Write([]byte(`[{"project_ref":"alpha","fqdn":"api.example.com","cert_status":"issued","cert_mode":"byo"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/domains/api.example.com":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	domain, err := client.AddProjectDomain(context.Background(), "alpha", "api.example.com")
	if err != nil {
		t.Fatalf("add project domain: %v", err)
	}
	if domain.ProjectRef != "alpha" || domain.FQDN != "api.example.com" || domain.CertStatus != "issued" || domain.CertMode != "byo" {
		t.Fatalf("unexpected domain %#v", domain)
	}
	domains, err := client.ListProjectDomains(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project domains: %v", err)
	}
	if len(domains) != 1 || domains[0].FQDN != "api.example.com" {
		t.Fatalf("unexpected domains %#v", domains)
	}
	if err := client.DeleteProjectDomain(context.Background(), "alpha", "api.example.com"); err != nil {
		t.Fatalf("delete project domain: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/domains",
		"GET /v1/projects/alpha/domains",
		"DELETE /v1/projects/alpha/domains/api.example.com",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestProjectDomainResourceStateComputesCustomEndpointURLs(t *testing.T) {
	var model projectDomainResourceModel
	setProjectDomainState(&model, ProjectDomain{
		ProjectRef: "alpha",
		FQDN:       "api.example.com",
		CertStatus: "uploaded",
		CertMode:   "byo",
	})

	if model.ID.ValueString() != "alpha/api.example.com" || model.CertMode.ValueString() != "byo" {
		t.Fatalf("unexpected identity/cert state: %#v", model)
	}
	for label, got := range map[string]string{
		"api":       model.APIURL.ValueString(),
		"ready_api": model.ReadyAPIURL.ValueString(),
		"rest":      model.RESTURL.ValueString(),
		"auth":      model.AuthURL.ValueString(),
		"graphql":   model.GraphQLURL.ValueString(),
		"realtime":  model.RealtimeURL.ValueString(),
		"functions": model.FunctionsURL.ValueString(),
		"storage":   model.StorageURL.ValueString(),
	} {
		if !strings.HasPrefix(got, "https://api.example.com") {
			t.Fatalf("%s URL = %q", label, got)
		}
	}

	setProjectDomainState(&model, ProjectDomain{
		ProjectRef: "alpha",
		FQDN:       "pending.example.com",
		CertStatus: "pending",
		CertMode:   "acme",
	})
	if model.APIURL.ValueString() != "https://pending.example.com" {
		t.Fatalf("pending api_url = %q", model.APIURL.ValueString())
	}
	if model.ReadyAPIURL.ValueString() != "" {
		t.Fatalf("pending ready_api_url should be empty, got %q", model.ReadyAPIURL.ValueString())
	}
}

func TestClientProjectConnectRequest(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet || r.URL.Path != "/v1/projects/alpha/connect" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
		_, _ = w.Write([]byte(`{
			"api_url":"https://alpha.apps.test",
			"studio_url":"https://studio-alpha.apps.test",
			"rest_url":"https://alpha.apps.test/rest/v1",
			"auth_url":"https://alpha.apps.test/auth/v1",
			"graphql_url":"https://alpha.apps.test/graphql/v1",
			"realtime_url":"https://alpha.apps.test/realtime/v1",
			"functions_url":"https://alpha.apps.test/functions/v1",
			"storage_url":"https://alpha.apps.test/storage/v1",
			"storage_s3_url":"https://storage-alpha.apps.test/storage/v1/s3",
			"custom_api_urls":["https://api.example.com"],
			"api_keys":{"anon":"secret://projects/alpha/anon_key","service_role":"secret://projects/alpha/service_role"},
			"postgres":{
				"public_direct":"postgres://postgres:${DB_PASSWORD}@db-alpha.apps.test:5432/postgres?sslmode=require",
				"public_transaction":"postgres://postgres.alpha:${DB_PASSWORD}@pooler-alpha.apps.test:6543/postgres?sslmode=require",
				"public_session":"postgres://postgres.alpha:${DB_PASSWORD}@pooler-alpha.apps.test:5432/postgres?sslmode=require"
			}
		}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	connect, err := client.GetProjectConnect(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("get project connect: %v", err)
	}
	if connect.APIURL != "https://alpha.apps.test" || connect.CustomAPIURLs[0] != "https://api.example.com" || connect.APIKeys["anon"] == "" || connect.Postgres["public_direct"] == "" {
		t.Fatalf("unexpected connect payload %#v", connect)
	}
	if strings.Join(seen, "\n") != "GET /v1/projects/alpha/connect" {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestProjectConnectDataSourceState(t *testing.T) {
	var model projectConnectDataSourceModel
	setProjectConnectDataSourceState(context.Background(), &model, "alpha", ProjectConnect{
		APIURL:        "https://alpha.apps.test",
		StudioURL:     "https://studio-alpha.apps.test",
		RESTURL:       "https://alpha.apps.test/rest/v1",
		AuthURL:       "https://alpha.apps.test/auth/v1",
		GraphQLURL:    "https://alpha.apps.test/graphql/v1",
		RealtimeURL:   "https://alpha.apps.test/realtime/v1",
		FunctionsURL:  "https://alpha.apps.test/functions/v1",
		StorageURL:    "https://alpha.apps.test/storage/v1",
		StorageS3URL:  "https://storage-alpha.apps.test/storage/v1/s3",
		CustomAPIURLs: []string{"https://api.example.com", "https://alt.example.com"},
		APIKeys:       map[string]string{"anon": "secret://projects/alpha/anon_key", "service_role": "secret://projects/alpha/service_role"},
		Postgres: map[string]string{
			"public_direct":      "postgres://postgres:${DB_PASSWORD}@db-alpha.apps.test:5432/postgres?sslmode=require",
			"public_transaction": "postgres://postgres.alpha:${DB_PASSWORD}@pooler-alpha.apps.test:6543/postgres?sslmode=require",
			"public_session":     "postgres://postgres.alpha:${DB_PASSWORD}@pooler-alpha.apps.test:5432/postgres?sslmode=require",
		},
	}, func(title string, detail string) {
		t.Fatalf("%s: %s", title, detail)
	})

	if model.Ref.ValueString() != "alpha" || model.APIURL.ValueString() != "https://alpha.apps.test" || model.AnonKeyHandle.ValueString() == "" {
		t.Fatalf("unexpected connect state %#v", model)
	}
	if model.PublicDatabaseURL.ValueString() == "" || model.PublicPoolerTransactionURL.ValueString() == "" || model.StorageS3URL.ValueString() == "" {
		t.Fatalf("missing remote connection fields %#v", model)
	}
	var customURLs []string
	diags := model.CustomAPIURLs.ElementsAs(context.Background(), &customURLs, false)
	if diags.HasError() {
		t.Fatalf("decode custom API URLs: %s", diags.Errors()[0].Detail())
	}
	if strings.Join(customURLs, ",") != "https://api.example.com,https://alt.example.com" {
		t.Fatalf("custom_api_urls = %#v", customURLs)
	}
}

func TestClientProjectLogDrainLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/log-drains":
			var got ProjectLogDrainInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Target != "https" || got.Config["url"] != "https://logs.example.com/ingest" || got.Config["token"] != "secret://projects/alpha/logs" {
				t.Fatalf("unexpected log drain payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"drain_1","project_ref":"alpha","target":"https","config":{"url":"https://logs.example.com/ingest","token":"********"},"created_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/log-drains":
			_, _ = w.Write([]byte(`[{"id":"drain_1","project_ref":"alpha","target":"https","config":{"url":"https://logs.example.com/ingest","token":"********"},"created_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/log-drains/drain_1":
			var got ProjectLogDrainInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Target != "loki" || got.Config["url"] != "https://loki.example.com/api/v1/push" {
				t.Fatalf("unexpected log drain update payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"drain_1","project_ref":"alpha","target":"loki","config":{"url":"https://loki.example.com/api/v1/push"},"created_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/log-drains/drain_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	drain, err := client.CreateProjectLogDrain(context.Background(), "alpha", ProjectLogDrainInput{
		Target: "https",
		Config: map[string]string{
			"url":   "https://logs.example.com/ingest",
			"token": "secret://projects/alpha/logs",
		},
	})
	if err != nil {
		t.Fatalf("create project log drain: %v", err)
	}
	if drain.ID != "drain_1" || drain.ProjectRef != "alpha" || drain.Config["token"] != "********" || drain.CreatedAt.IsZero() {
		t.Fatalf("unexpected log drain %#v", drain)
	}
	drains, err := client.ListProjectLogDrains(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project log drains: %v", err)
	}
	if len(drains) != 1 || drains[0].ID != "drain_1" || drains[0].Target != "https" {
		t.Fatalf("unexpected log drains %#v", drains)
	}
	updated, err := client.UpdateProjectLogDrain(context.Background(), "alpha", "drain_1", ProjectLogDrainInput{
		Target: "loki",
		Config: map[string]string{"url": "https://loki.example.com/api/v1/push"},
	})
	if err != nil {
		t.Fatalf("update project log drain: %v", err)
	}
	if updated.ID != "drain_1" || updated.Target != "loki" {
		t.Fatalf("unexpected updated log drain %#v", updated)
	}
	if err := client.DeleteProjectLogDrain(context.Background(), "alpha", "drain_1"); err != nil {
		t.Fatalf("delete project log drain: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/log-drains",
		"GET /v1/projects/alpha/log-drains",
		"PUT /v1/projects/alpha/log-drains/drain_1",
		"DELETE /v1/projects/alpha/log-drains/drain_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectNetworkConnectionLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/network-connections":
			var got ProjectNetworkConnectionInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "aws-prod" || got.Type != "privatelink" || got.Provider != "aws" || got.Region != "us-east-1" || got.EndpointID != "vpce-123" {
				t.Fatalf("unexpected network connection payload %#v", got)
			}
			if len(got.CIDRs) != 2 || got.CIDRs[0] != "10.0.0.0/16" || got.CIDRs[1] != "203.0.113.10" {
				t.Fatalf("unexpected network connection cidrs %#v", got.CIDRs)
			}
			if got.Config["account_id"] != "123456789012" || got.Config["token"] != "secret://projects/alpha/private-link-token" {
				t.Fatalf("unexpected network connection config %#v", got.Config)
			}
			_, _ = w.Write([]byte(`{"id":"net_1","project_ref":"alpha","name":"aws-prod","type":"privatelink","provider":"aws","region":"us-east-1","cidrs":["10.0.0.0/16","203.0.113.10"],"endpoint_id":"vpce-123","config":{"account_id":"123456789012","token":"********"},"status":"requested","message":"private network connection declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/network-connections":
			_, _ = w.Write([]byte(`[{"id":"net_1","project_ref":"alpha","name":"aws-prod","type":"privatelink","provider":"aws","region":"us-east-1","cidrs":["10.0.0.0/16","203.0.113.10"],"endpoint_id":"vpce-123","config":{"account_id":"123456789012","token":"********"},"status":"requested","message":"private network connection declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/network-connections/net_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	connection, err := client.CreateProjectNetworkConnection(context.Background(), "alpha", ProjectNetworkConnectionInput{
		Name:       "aws-prod",
		Type:       "privatelink",
		Provider:   "aws",
		Region:     "us-east-1",
		CIDRs:      []string{"10.0.0.0/16", "203.0.113.10"},
		EndpointID: "vpce-123",
		Config: map[string]string{
			"account_id": "123456789012",
			"token":      "secret://projects/alpha/private-link-token",
		},
	})
	if err != nil {
		t.Fatalf("create project network connection: %v", err)
	}
	if connection.ID != "net_1" || connection.ProjectRef != "alpha" || connection.Status != "requested" || connection.Config["token"] != "********" || connection.CreatedAt.IsZero() || connection.UpdatedAt.IsZero() {
		t.Fatalf("unexpected network connection %#v", connection)
	}
	connections, err := client.ListProjectNetworkConnections(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project network connections: %v", err)
	}
	if len(connections) != 1 || connections[0].ID != "net_1" || connections[0].Name != "aws-prod" {
		t.Fatalf("unexpected network connections %#v", connections)
	}
	if err := client.DeleteProjectNetworkConnection(context.Background(), "alpha", "net_1"); err != nil {
		t.Fatalf("delete project network connection: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/network-connections",
		"GET /v1/projects/alpha/network-connections",
		"DELETE /v1/projects/alpha/network-connections/net_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectStorageBucketLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/storage/buckets":
			var got ProjectStorageBucketInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "assets" || !got.Public || got.FileSizeLimit != 1048576 || got.CacheControl != "600" || !got.AvifAutodetection {
				t.Fatalf("unexpected storage bucket payload %#v", got)
			}
			if strings.Join(got.AllowedMimeTypes, ",") != "image/png,image/jpeg" {
				t.Fatalf("unexpected storage bucket mime types %#v", got.AllowedMimeTypes)
			}
			if got.Metadata["purpose"] != "public-assets" || got.Metadata["token"] != "secret://projects/alpha/storage-token" {
				t.Fatalf("unexpected storage bucket metadata %#v", got.Metadata)
			}
			_, _ = w.Write([]byte(`{"id":"bucket_1","project_ref":"alpha","name":"assets","public":true,"file_size_limit":1048576,"allowed_mime_types":["image/jpeg","image/png"],"cache_control":"600","avif_autodetection":true,"metadata":{"purpose":"public-assets","token":"********"},"status":"configured","message":"storage bucket declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/storage/buckets":
			_, _ = w.Write([]byte(`[{"id":"bucket_1","project_ref":"alpha","name":"assets","public":true,"file_size_limit":1048576,"allowed_mime_types":["image/jpeg","image/png"],"cache_control":"600","avif_autodetection":true,"metadata":{"purpose":"public-assets","token":"********"},"status":"configured","message":"storage bucket declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/storage/buckets/assets":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.CreateProjectStorageBucket(context.Background(), "alpha", ProjectStorageBucketInput{
		Name:              "assets",
		Public:            true,
		FileSizeLimit:     1048576,
		AllowedMimeTypes:  []string{"image/png", "image/jpeg"},
		CacheControl:      "600",
		AvifAutodetection: true,
		Metadata: map[string]string{
			"purpose": "public-assets",
			"token":   "secret://projects/alpha/storage-token",
		},
	})
	if err != nil {
		t.Fatalf("create project storage bucket: %v", err)
	}
	if bucket.ID != "bucket_1" || bucket.ProjectRef != "alpha" || bucket.Name != "assets" || bucket.Metadata["token"] != "********" || bucket.CreatedAt.IsZero() || bucket.UpdatedAt.IsZero() {
		t.Fatalf("unexpected storage bucket %#v", bucket)
	}
	buckets, err := client.ListProjectStorageBuckets(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project storage buckets: %v", err)
	}
	if len(buckets) != 1 || buckets[0].Name != "assets" || !buckets[0].Public {
		t.Fatalf("unexpected storage buckets %#v", buckets)
	}
	if err := client.DeleteProjectStorageBucket(context.Background(), "alpha", "assets"); err != nil {
		t.Fatalf("delete project storage bucket: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/storage/buckets",
		"GET /v1/projects/alpha/storage/buckets",
		"DELETE /v1/projects/alpha/storage/buckets/assets",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectVectorBucketLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/vector-buckets":
			var got ProjectVectorBucketInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "documents" || got.Dimension != 1536 || got.Distance != "cosine" || got.IndexMethod != "hnsw" || got.StorageBackend != "s3" || got.StorageURI != "s3://vectors/documents" {
				t.Fatalf("unexpected vector bucket payload %#v", got)
			}
			if got.Metadata["purpose"] != "search" || got.Metadata["access_key"] != "secret://projects/alpha/vector-s3" {
				t.Fatalf("unexpected vector bucket metadata %#v", got.Metadata)
			}
			_, _ = w.Write([]byte(`{"id":"vb_1","project_ref":"alpha","name":"documents","dimension":1536,"distance":"cosine","index_method":"hnsw","storage_backend":"s3","storage_uri":"s3://vectors/documents","metadata":{"purpose":"search","access_key":"********"},"status":"configured","message":"vector bucket declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/vector-buckets":
			_, _ = w.Write([]byte(`[{"id":"vb_1","project_ref":"alpha","name":"documents","dimension":1536,"distance":"cosine","index_method":"hnsw","storage_backend":"s3","storage_uri":"s3://vectors/documents","metadata":{"purpose":"search","access_key":"********"},"status":"configured","message":"vector bucket declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/vector-buckets/documents":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.CreateProjectVectorBucket(context.Background(), "alpha", ProjectVectorBucketInput{
		Name:           "documents",
		Dimension:      1536,
		Distance:       "cosine",
		IndexMethod:    "hnsw",
		StorageBackend: "s3",
		StorageURI:     "s3://vectors/documents",
		Metadata: map[string]string{
			"purpose":    "search",
			"access_key": "secret://projects/alpha/vector-s3",
		},
	})
	if err != nil {
		t.Fatalf("create project vector bucket: %v", err)
	}
	if bucket.ID != "vb_1" || bucket.ProjectRef != "alpha" || bucket.Name != "documents" || bucket.Metadata["access_key"] != "********" || bucket.CreatedAt.IsZero() || bucket.UpdatedAt.IsZero() {
		t.Fatalf("unexpected vector bucket %#v", bucket)
	}
	buckets, err := client.ListProjectVectorBuckets(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project vector buckets: %v", err)
	}
	if len(buckets) != 1 || buckets[0].Name != "documents" || buckets[0].StorageBackend != "s3" {
		t.Fatalf("unexpected vector buckets %#v", buckets)
	}
	if err := client.DeleteProjectVectorBucket(context.Background(), "alpha", "documents"); err != nil {
		t.Fatalf("delete project vector bucket: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/vector-buckets",
		"GET /v1/projects/alpha/vector-buckets",
		"DELETE /v1/projects/alpha/vector-buckets/documents",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectAnalyticsBucketLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/analytics-buckets":
			var got ProjectAnalyticsBucketInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "events" || got.StorageURI != "s3://lakehouse/events" || got.CatalogURI != "http://iceberg-rest:8181" || got.Warehouse != "analytics" || got.CredentialHandle != "secret://projects/alpha/iceberg" || got.FormatVersion != 2 || got.Partitioning != "days(created_at)" || got.RetentionDays != 365 || got.CompactionSchedule != "0 2 * * *" {
				t.Fatalf("unexpected analytics bucket payload %#v", got)
			}
			if got.Metadata["purpose"] != "warehouse" || got.Metadata["access_key"] != "secret://projects/alpha/s3" {
				t.Fatalf("unexpected analytics bucket metadata %#v", got.Metadata)
			}
			_, _ = w.Write([]byte(`{"id":"ab_1","project_ref":"alpha","name":"events","storage_uri":"s3://lakehouse/events","catalog_uri":"http://iceberg-rest:8181","warehouse":"analytics","credential_handle":"********","format_version":2,"partitioning":"days(created_at)","retention_days":365,"compaction_schedule":"0 2 * * *","metadata":{"purpose":"warehouse","access_key":"********"},"status":"configured","message":"Iceberg analytics bucket declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/analytics-buckets":
			_, _ = w.Write([]byte(`[{"id":"ab_1","project_ref":"alpha","name":"events","storage_uri":"s3://lakehouse/events","catalog_uri":"http://iceberg-rest:8181","warehouse":"analytics","credential_handle":"********","format_version":2,"partitioning":"days(created_at)","retention_days":365,"compaction_schedule":"0 2 * * *","metadata":{"purpose":"warehouse","access_key":"********"},"status":"configured","message":"Iceberg analytics bucket declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/analytics-buckets/events":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	bucket, err := client.CreateProjectAnalyticsBucket(context.Background(), "alpha", ProjectAnalyticsBucketInput{
		Name:               "events",
		StorageURI:         "s3://lakehouse/events",
		CatalogURI:         "http://iceberg-rest:8181",
		Warehouse:          "analytics",
		CredentialHandle:   "secret://projects/alpha/iceberg",
		FormatVersion:      2,
		Partitioning:       "days(created_at)",
		RetentionDays:      365,
		CompactionSchedule: "0 2 * * *",
		Metadata: map[string]string{
			"purpose":    "warehouse",
			"access_key": "secret://projects/alpha/s3",
		},
	})
	if err != nil {
		t.Fatalf("create project analytics bucket: %v", err)
	}
	if bucket.ID != "ab_1" || bucket.ProjectRef != "alpha" || bucket.Name != "events" || bucket.CredentialHandle != "********" || bucket.Metadata["access_key"] != "********" || bucket.CreatedAt.IsZero() || bucket.UpdatedAt.IsZero() {
		t.Fatalf("unexpected analytics bucket %#v", bucket)
	}
	buckets, err := client.ListProjectAnalyticsBuckets(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project analytics buckets: %v", err)
	}
	if len(buckets) != 1 || buckets[0].Name != "events" || buckets[0].FormatVersion != 2 {
		t.Fatalf("unexpected analytics buckets %#v", buckets)
	}
	if err := client.DeleteProjectAnalyticsBucket(context.Background(), "alpha", "events"); err != nil {
		t.Fatalf("delete project analytics bucket: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/analytics-buckets",
		"GET /v1/projects/alpha/analytics-buckets",
		"DELETE /v1/projects/alpha/analytics-buckets/events",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectReplicationPipelineLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/replication":
			var got ProjectReplicationPipelineInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "orders-etl" || got.Type != "etl" || got.SourceSchema != "public" || got.SourceTable != "orders" || got.Destination != "s3" || got.DestinationURI != "s3://lake/orders" || got.CredentialHandle != "secret://projects/alpha/etl" {
				t.Fatalf("unexpected replication pipeline payload %#v", got)
			}
			if got.Config["bucket"] != "lake" || got.Config["access_key"] != "secret://projects/alpha/s3" {
				t.Fatalf("unexpected replication pipeline config %#v", got.Config)
			}
			_, _ = w.Write([]byte(`{"id":"pipe_1","project_ref":"alpha","name":"orders-etl","type":"etl","source_schema":"public","source_table":"orders","destination":"s3","destination_uri":"s3://lake/orders","credential_handle":"********","config":{"bucket":"lake","access_key":"********"},"status":"configured","message":"declarative replication pipeline recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/replication":
			_, _ = w.Write([]byte(`[{"id":"pipe_1","project_ref":"alpha","name":"orders-etl","type":"etl","source_schema":"public","source_table":"orders","destination":"s3","destination_uri":"s3://lake/orders","credential_handle":"********","config":{"bucket":"lake","access_key":"********"},"status":"configured","message":"declarative replication pipeline recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/replication/pipe_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := client.CreateProjectReplicationPipeline(context.Background(), "alpha", ProjectReplicationPipelineInput{
		Name:             "orders-etl",
		Type:             "etl",
		SourceSchema:     "public",
		SourceTable:      "orders",
		Destination:      "s3",
		DestinationURI:   "s3://lake/orders",
		CredentialHandle: "secret://projects/alpha/etl",
		Config: map[string]string{
			"bucket":     "lake",
			"access_key": "secret://projects/alpha/s3",
		},
	})
	if err != nil {
		t.Fatalf("create project replication pipeline: %v", err)
	}
	if pipeline.ID != "pipe_1" || pipeline.ProjectRef != "alpha" || pipeline.CredentialHandle != "********" || pipeline.Config["access_key"] != "********" || pipeline.CreatedAt.IsZero() || pipeline.UpdatedAt.IsZero() {
		t.Fatalf("unexpected replication pipeline %#v", pipeline)
	}
	pipelines, err := client.ListProjectReplicationPipelines(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project replication pipelines: %v", err)
	}
	if len(pipelines) != 1 || pipelines[0].Name != "orders-etl" || pipelines[0].Destination != "s3" {
		t.Fatalf("unexpected replication pipelines %#v", pipelines)
	}
	if err := client.DeleteProjectReplicationPipeline(context.Background(), "alpha", "pipe_1"); err != nil {
		t.Fatalf("delete project replication pipeline: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/replication",
		"GET /v1/projects/alpha/replication",
		"DELETE /v1/projects/alpha/replication/pipe_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectCDNPolicyLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/cdn/policy":
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":false,"browser_ttl_seconds":3600,"edge_ttl_seconds":3600,"stale_while_revalidate_seconds":60,"included_paths":["/storage/v1/object/public/*"],"excluded_paths":[],"smart_revalidation":false,"cache_control":"public, max-age=3600, s-maxage=3600, stale-while-revalidate=60","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/cdn/policy":
			var got ProjectCDNPolicyInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if !got.Enabled || got.BrowserTTLSeconds != 300 || got.EdgeTTLSeconds != 600 || got.StaleWhileRevalidateSeconds != 30 || !got.SmartRevalidation || got.CacheControl != "public, max-age=300, s-maxage=600, stale-while-revalidate=30" {
				t.Fatalf("unexpected cdn policy payload %#v", got)
			}
			if strings.Join(got.IncludedPaths, ",") != "/storage/*" || strings.Join(got.ExcludedPaths, ",") != "/storage/private/*" {
				t.Fatalf("unexpected cdn policy paths %#v %#v", got.IncludedPaths, got.ExcludedPaths)
			}
			_, _ = w.Write([]byte(`{"project_ref":"alpha","enabled":true,"browser_ttl_seconds":300,"edge_ttl_seconds":600,"stale_while_revalidate_seconds":30,"included_paths":["/storage/*"],"excluded_paths":["/storage/private/*"],"smart_revalidation":true,"cache_control":"public, max-age=300, s-maxage=600, stale-while-revalidate=30","updated_at":"2026-06-05T12:01:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	current, err := client.GetProjectCDNPolicy(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("get project cdn policy: %v", err)
	}
	if current.ProjectRef != "alpha" || current.Enabled || current.CacheControl == "" || current.UpdatedAt.IsZero() {
		t.Fatalf("unexpected current cdn policy %#v", current)
	}
	updated, err := client.UpdateProjectCDNPolicy(context.Background(), "alpha", ProjectCDNPolicyInput{
		Enabled:                     true,
		BrowserTTLSeconds:           300,
		EdgeTTLSeconds:              600,
		StaleWhileRevalidateSeconds: 30,
		IncludedPaths:               []string{"/storage/*"},
		ExcludedPaths:               []string{"/storage/private/*"},
		SmartRevalidation:           true,
		CacheControl:                "public, max-age=300, s-maxage=600, stale-while-revalidate=30",
	})
	if err != nil {
		t.Fatalf("update project cdn policy: %v", err)
	}
	if !updated.Enabled || !updated.SmartRevalidation || updated.EdgeTTLSeconds != 600 || updated.CacheControl == "" {
		t.Fatalf("unexpected updated cdn policy %#v", updated)
	}

	expected := []string{
		"GET /v1/projects/alpha/cdn/policy",
		"PUT /v1/projects/alpha/cdn/policy",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectAuthClientLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/auth/clients":
			var got ProjectAuthClientInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "Dashboard App" || got.ClientID != "dashboard_app" || got.ClientSecretHandle != "secret://projects/alpha/auth/dashboard" || !got.Confidential {
				t.Fatalf("unexpected auth client payload %#v", got)
			}
			if strings.Join(got.RedirectURIs, ",") != "https://app.example.com/auth/callback,https://app.example.com/auth/alt" {
				t.Fatalf("unexpected auth client redirect uris %#v", got.RedirectURIs)
			}
			if strings.Join(got.GrantTypes, ",") != "authorization_code,refresh_token" || strings.Join(got.Scopes, ",") != "openid,email,profile" {
				t.Fatalf("unexpected auth client grant/scopes %#v %#v", got.GrantTypes, got.Scopes)
			}
			_, _ = w.Write([]byte(`{"id":"ac_1","project_ref":"alpha","name":"Dashboard App","client_id":"dashboard_app","client_secret_handle":"********","redirect_uris":["https://app.example.com/auth/callback","https://app.example.com/auth/alt"],"grant_types":["authorization_code","refresh_token"],"scopes":["openid","email","profile"],"confidential":true,"status":"registered","message":"OAuth 2.1 client registration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/auth/clients":
			_, _ = w.Write([]byte(`[{"id":"ac_1","project_ref":"alpha","name":"Dashboard App","client_id":"dashboard_app","client_secret_handle":"********","redirect_uris":["https://app.example.com/auth/callback","https://app.example.com/auth/alt"],"grant_types":["authorization_code","refresh_token"],"scopes":["openid","email","profile"],"confidential":true,"status":"registered","message":"OAuth 2.1 client registration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/auth/clients/dashboard_app":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	authClient, err := client.CreateProjectAuthClient(context.Background(), "alpha", ProjectAuthClientInput{
		Name:               "Dashboard App",
		ClientID:           "dashboard_app",
		ClientSecretHandle: "secret://projects/alpha/auth/dashboard",
		RedirectURIs:       []string{"https://app.example.com/auth/callback", "https://app.example.com/auth/alt"},
		GrantTypes:         []string{"authorization_code", "refresh_token"},
		Scopes:             []string{"openid", "email", "profile"},
		Confidential:       true,
	})
	if err != nil {
		t.Fatalf("create project auth client: %v", err)
	}
	if authClient.ID != "ac_1" || authClient.ProjectRef != "alpha" || authClient.ClientSecretHandle != "********" || authClient.CreatedAt.IsZero() || authClient.UpdatedAt.IsZero() {
		t.Fatalf("unexpected auth client %#v", authClient)
	}
	clients, err := client.ListProjectAuthClients(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project auth clients: %v", err)
	}
	if len(clients) != 1 || clients[0].ClientID != "dashboard_app" || !clients[0].Confidential {
		t.Fatalf("unexpected auth clients %#v", clients)
	}
	if err := client.DeleteProjectAuthClient(context.Background(), "alpha", "dashboard_app"); err != nil {
		t.Fatalf("delete project auth client: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/auth/clients",
		"GET /v1/projects/alpha/auth/clients",
		"DELETE /v1/projects/alpha/auth/clients/dashboard_app",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectAuthHookLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/auth/hooks":
			var got ProjectAuthHookInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.HookType != "custom_access_token" || !got.Enabled || got.TargetURI != "https://hooks.example.com/custom-token" || got.SecretHandle != "secret://projects/alpha/auth-hook" {
				t.Fatalf("unexpected auth hook payload %#v", got)
			}
			if got.EdgeFunction != "" || got.TimeoutMS != 3000 || got.RetryAttempts != 2 {
				t.Fatalf("unexpected auth hook target/timing %#v", got)
			}
			if got.Headers["Authorization"] != "secret://projects/alpha/hook-token" || got.Headers["X-Supadupa-Env"] != "prod" {
				t.Fatalf("unexpected auth hook headers %#v", got.Headers)
			}
			_, _ = w.Write([]byte(`{"id":"ah_1","project_ref":"alpha","hook_type":"custom_access_token","enabled":true,"target_uri":"https://hooks.example.com/custom-token","secret_handle":"********","headers":{"Authorization":"********","X-Supadupa-Env":"prod"},"timeout_ms":3000,"retry_attempts":2,"status":"configured","message":"Auth hook declaration ready for runtime sync","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/auth/hooks":
			_, _ = w.Write([]byte(`[{"id":"ah_1","project_ref":"alpha","hook_type":"custom_access_token","enabled":true,"target_uri":"https://hooks.example.com/custom-token","secret_handle":"********","headers":{"Authorization":"********","X-Supadupa-Env":"prod"},"timeout_ms":3000,"retry_attempts":2,"status":"configured","message":"Auth hook declaration ready for runtime sync","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/auth/hooks/custom_access_token":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	hook, err := client.CreateProjectAuthHook(context.Background(), "alpha", ProjectAuthHookInput{
		HookType:      "custom_access_token",
		Enabled:       true,
		TargetURI:     "https://hooks.example.com/custom-token",
		SecretHandle:  "secret://projects/alpha/auth-hook",
		Headers:       map[string]string{"Authorization": "secret://projects/alpha/hook-token", "X-Supadupa-Env": "prod"},
		TimeoutMS:     3000,
		RetryAttempts: 2,
	})
	if err != nil {
		t.Fatalf("create project auth hook: %v", err)
	}
	if hook.ID != "ah_1" || hook.ProjectRef != "alpha" || hook.SecretHandle != "********" || hook.Headers["Authorization"] != "********" || hook.CreatedAt.IsZero() || hook.UpdatedAt.IsZero() {
		t.Fatalf("unexpected auth hook %#v", hook)
	}
	hooks, err := client.ListProjectAuthHooks(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project auth hooks: %v", err)
	}
	if len(hooks) != 1 || hooks[0].HookType != "custom_access_token" || !hooks[0].Enabled || hooks[0].TimeoutMS != 3000 {
		t.Fatalf("unexpected auth hooks %#v", hooks)
	}
	if err := client.DeleteProjectAuthHook(context.Background(), "alpha", "custom_access_token"); err != nil {
		t.Fatalf("delete project auth hook: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/auth/hooks",
		"GET /v1/projects/alpha/auth/hooks",
		"DELETE /v1/projects/alpha/auth/hooks/custom_access_token",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectDatabaseCronJobLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/cron-jobs":
			var got ProjectDatabaseCronJobInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "refresh-rollups" || got.Schedule != "*/15 * * * *" || got.Command != "select analytics.refresh_rollups();" {
				t.Fatalf("unexpected database cron payload %#v", got)
			}
			if got.Database != "postgres" || got.Username != "postgres" || !got.Active || got.TimeoutSeconds != 90 || got.MaxRuntimeSeconds != 120 {
				t.Fatalf("unexpected database cron target/runtime %#v", got)
			}
			if got.Metadata["owner"] != "analytics" || got.Metadata["password"] != "secret://projects/alpha/db/cron-password" {
				t.Fatalf("unexpected database cron metadata %#v", got.Metadata)
			}
			_, _ = w.Write([]byte(`{"id":"cron_1","project_ref":"alpha","name":"refresh-rollups","schedule":"*/15 * * * *","command":"select analytics.refresh_rollups();","database":"postgres","username":"postgres","active":true,"timeout_seconds":90,"max_runtime_seconds":120,"metadata":{"owner":"analytics","password":"********"},"status":"scheduled","message":"pg_cron job declaration ready for runtime sync","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/cron-jobs":
			_, _ = w.Write([]byte(`[{"id":"cron_1","project_ref":"alpha","name":"refresh-rollups","schedule":"*/15 * * * *","command":"select analytics.refresh_rollups();","database":"postgres","username":"postgres","active":true,"timeout_seconds":90,"max_runtime_seconds":120,"metadata":{"owner":"analytics","password":"********"},"status":"scheduled","message":"pg_cron job declaration ready for runtime sync","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/cron-jobs/refresh-rollups":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	job, err := client.CreateProjectDatabaseCronJob(context.Background(), "alpha", ProjectDatabaseCronJobInput{
		Name:              "refresh-rollups",
		Schedule:          "*/15 * * * *",
		Command:           "select analytics.refresh_rollups();",
		Database:          "postgres",
		Username:          "postgres",
		Active:            true,
		TimeoutSeconds:    90,
		MaxRuntimeSeconds: 120,
		Metadata:          map[string]string{"owner": "analytics", "password": "secret://projects/alpha/db/cron-password"},
	})
	if err != nil {
		t.Fatalf("create project database cron job: %v", err)
	}
	if job.ID != "cron_1" || job.ProjectRef != "alpha" || job.Metadata["password"] != "********" || job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() {
		t.Fatalf("unexpected database cron job %#v", job)
	}
	jobs, err := client.ListProjectDatabaseCronJobs(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project database cron jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Name != "refresh-rollups" || !jobs[0].Active || jobs[0].MaxRuntimeSeconds != 120 {
		t.Fatalf("unexpected database cron jobs %#v", jobs)
	}
	if err := client.DeleteProjectDatabaseCronJob(context.Background(), "alpha", "refresh-rollups"); err != nil {
		t.Fatalf("delete project database cron job: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/database/cron-jobs",
		"GET /v1/projects/alpha/database/cron-jobs",
		"DELETE /v1/projects/alpha/database/cron-jobs/refresh-rollups",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectDatabaseQueueLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/queues":
			var got ProjectDatabaseQueueInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "events" || got.Schema != "pgmq" || got.RetentionMinutes != 10080 || got.VisibilityTimeoutSeconds != 45 {
				t.Fatalf("unexpected database queue payload %#v", got)
			}
			if got.MaxRetries != 7 || got.DeadLetterQueue != "events-dlq" || !got.Active {
				t.Fatalf("unexpected database queue retry policy %#v", got)
			}
			if got.Metadata["owner"] != "backend" || got.Metadata["token"] != "secret://projects/alpha/db/pgmq-token" {
				t.Fatalf("unexpected database queue metadata %#v", got.Metadata)
			}
			_, _ = w.Write([]byte(`{"id":"queue_1","project_ref":"alpha","name":"events","schema":"pgmq","retention_minutes":10080,"visibility_timeout_seconds":45,"max_retries":7,"dead_letter_queue":"events-dlq","active":true,"metadata":{"owner":"backend","token":"********"},"status":"ready","message":"pgmq queue declaration ready for runtime sync","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/queues":
			_, _ = w.Write([]byte(`[{"id":"queue_1","project_ref":"alpha","name":"events","schema":"pgmq","retention_minutes":10080,"visibility_timeout_seconds":45,"max_retries":7,"dead_letter_queue":"events-dlq","active":true,"metadata":{"owner":"backend","token":"********"},"status":"ready","message":"pgmq queue declaration ready for runtime sync","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/queues/events":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	queue, err := client.CreateProjectDatabaseQueue(context.Background(), "alpha", ProjectDatabaseQueueInput{
		Name:                     "events",
		Schema:                   "pgmq",
		RetentionMinutes:         10080,
		VisibilityTimeoutSeconds: 45,
		MaxRetries:               7,
		DeadLetterQueue:          "events-dlq",
		Active:                   true,
		Metadata:                 map[string]string{"owner": "backend", "token": "secret://projects/alpha/db/pgmq-token"},
	})
	if err != nil {
		t.Fatalf("create project database queue: %v", err)
	}
	if queue.ID != "queue_1" || queue.ProjectRef != "alpha" || queue.Metadata["token"] != "********" || queue.CreatedAt.IsZero() || queue.UpdatedAt.IsZero() {
		t.Fatalf("unexpected database queue %#v", queue)
	}
	queues, err := client.ListProjectDatabaseQueues(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project database queues: %v", err)
	}
	if len(queues) != 1 || queues[0].Name != "events" || !queues[0].Active || queues[0].DeadLetterQueue != "events-dlq" {
		t.Fatalf("unexpected database queues %#v", queues)
	}
	if err := client.DeleteProjectDatabaseQueue(context.Background(), "alpha", "events"); err != nil {
		t.Fatalf("delete project database queue: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/database/queues",
		"GET /v1/projects/alpha/database/queues",
		"DELETE /v1/projects/alpha/database/queues/events",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectDatabaseWebhookLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/webhooks":
			var got ProjectDatabaseWebhookInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "orders-events" || got.Schema != "public" || got.Table != "orders" || got.Endpoint != "https://hooks.example.com/orders" {
				t.Fatalf("unexpected database webhook payload %#v", got)
			}
			if strings.Join(got.Events, ",") != "insert,update" || got.HTTPMethod != "POST" || got.TimeoutSeconds != 15 || got.RetryCount != 5 || !got.Active {
				t.Fatalf("unexpected database webhook delivery config %#v", got)
			}
			if got.Headers["Authorization"] != "secret://projects/alpha/webhooks/orders-token" || got.Headers["X-Source"] != "supadupa" {
				t.Fatalf("unexpected database webhook headers %#v", got.Headers)
			}
			if got.Metadata["owner"] != "backend" || got.Metadata["token"] != "secret://projects/alpha/webhooks/meta-token" {
				t.Fatalf("unexpected database webhook metadata %#v", got.Metadata)
			}
			_, _ = w.Write([]byte(`{"id":"webhook_1","project_ref":"alpha","name":"orders-events","schema":"public","table":"orders","events":["insert","update"],"endpoint":"https://hooks.example.com/orders","http_method":"POST","headers":{"Authorization":"********","X-Source":"supadupa"},"timeout_seconds":15,"retry_count":5,"active":true,"metadata":{"owner":"backend","token":"********"},"status":"ready","message":"database webhook declaration ready for runtime sync","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/webhooks":
			_, _ = w.Write([]byte(`[{"id":"webhook_1","project_ref":"alpha","name":"orders-events","schema":"public","table":"orders","events":["insert","update"],"endpoint":"https://hooks.example.com/orders","http_method":"POST","headers":{"Authorization":"********","X-Source":"supadupa"},"timeout_seconds":15,"retry_count":5,"active":true,"metadata":{"owner":"backend","token":"********"},"status":"ready","message":"database webhook declaration ready for runtime sync","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/webhooks/orders-events":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	webhook, err := client.CreateProjectDatabaseWebhook(context.Background(), "alpha", ProjectDatabaseWebhookInput{
		Name:           "orders-events",
		Schema:         "public",
		Table:          "orders",
		Events:         []string{"insert", "update"},
		Endpoint:       "https://hooks.example.com/orders",
		HTTPMethod:     "POST",
		Headers:        map[string]string{"Authorization": "secret://projects/alpha/webhooks/orders-token", "X-Source": "supadupa"},
		TimeoutSeconds: 15,
		RetryCount:     5,
		Active:         true,
		Metadata:       map[string]string{"owner": "backend", "token": "secret://projects/alpha/webhooks/meta-token"},
	})
	if err != nil {
		t.Fatalf("create project database webhook: %v", err)
	}
	if webhook.ID != "webhook_1" || webhook.ProjectRef != "alpha" || webhook.Headers["Authorization"] != "********" || webhook.Metadata["token"] != "********" || webhook.CreatedAt.IsZero() || webhook.UpdatedAt.IsZero() {
		t.Fatalf("unexpected database webhook %#v", webhook)
	}
	webhooks, err := client.ListProjectDatabaseWebhooks(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project database webhooks: %v", err)
	}
	if len(webhooks) != 1 || webhooks[0].Name != "orders-events" || strings.Join(webhooks[0].Events, ",") != "insert,update" || !webhooks[0].Active {
		t.Fatalf("unexpected database webhooks %#v", webhooks)
	}
	if err := client.DeleteProjectDatabaseWebhook(context.Background(), "alpha", "orders-events"); err != nil {
		t.Fatalf("delete project database webhook: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/database/webhooks",
		"GET /v1/projects/alpha/database/webhooks",
		"DELETE /v1/projects/alpha/database/webhooks/orders-events",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectDatabaseSchemaLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/schemas":
			var got ProjectDatabaseSchemaInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "app-schema" || got.Version != "20260605_001" || got.Schema != "public" || got.SQL != "create table public.accounts(id uuid primary key);" {
				t.Fatalf("unexpected database schema payload %#v", got)
			}
			if got.ApplyOrder != 10 || !got.Active || got.Metadata["owner"] != "backend" || got.Metadata["token"] != "secret://projects/alpha/db/schema-token" {
				t.Fatalf("unexpected database schema options %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"schema_1","project_ref":"alpha","name":"app-schema","version":"20260605_001","schema":"public","sql":"create table public.accounts(id uuid primary key);","checksum":"6a4936e549a0a8622e1f9ab5e81857727f9456cc2e0a07802d087321b83a80f9","apply_order":10,"active":true,"metadata":{"owner":"backend","token":"********"},"status":"pending","message":"declarative schema migration ready for runtime sync","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/schemas":
			_, _ = w.Write([]byte(`[{"id":"schema_1","project_ref":"alpha","name":"app-schema","version":"20260605_001","schema":"public","sql":"create table public.accounts(id uuid primary key);","checksum":"6a4936e549a0a8622e1f9ab5e81857727f9456cc2e0a07802d087321b83a80f9","apply_order":10,"active":true,"metadata":{"owner":"backend","token":"********"},"status":"pending","message":"declarative schema migration ready for runtime sync","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/schemas/app-schema/20260605_001":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	schema, err := client.CreateProjectDatabaseSchema(context.Background(), "alpha", ProjectDatabaseSchemaInput{
		Name:       "app-schema",
		Version:    "20260605_001",
		Schema:     "public",
		SQL:        "create table public.accounts(id uuid primary key);",
		ApplyOrder: 10,
		Active:     true,
		Metadata:   map[string]string{"owner": "backend", "token": "secret://projects/alpha/db/schema-token"},
	})
	if err != nil {
		t.Fatalf("create project database schema: %v", err)
	}
	if schema.ID != "schema_1" || schema.ProjectRef != "alpha" || schema.Checksum == "" || schema.Metadata["token"] != "********" || schema.CreatedAt.IsZero() || schema.UpdatedAt.IsZero() {
		t.Fatalf("unexpected database schema %#v", schema)
	}
	schemas, err := client.ListProjectDatabaseSchemas(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project database schemas: %v", err)
	}
	if len(schemas) != 1 || schemas[0].Name != "app-schema" || schemas[0].Version != "20260605_001" || !schemas[0].Active {
		t.Fatalf("unexpected database schemas %#v", schemas)
	}
	if err := client.DeleteProjectDatabaseSchema(context.Background(), "alpha", "app-schema", "20260605_001"); err != nil {
		t.Fatalf("delete project database schema: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/database/schemas",
		"GET /v1/projects/alpha/database/schemas",
		"DELETE /v1/projects/alpha/database/schemas/app-schema/20260605_001",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectDatabaseRoleLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/database/roles":
			var got ProjectDatabaseRoleInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "app_writer" || !got.Login || got.Inherit == nil || *got.Inherit || !got.BypassRLS || got.ConnectionLimit != 25 {
				t.Fatalf("unexpected database role payload %#v", got)
			}
			if got.PasswordSecretHandle != "secret://projects/alpha/db/app-writer" || strings.Join(got.MemberOf, ",") != "authenticated" {
				t.Fatalf("unexpected database role auth/membership %#v", got)
			}
			if got.SchemaGrants["public"] != "usage,select,insert" || got.Metadata["purpose"] != "app" || got.Metadata["api_key"] != "secret://projects/alpha/db-role-api" {
				t.Fatalf("unexpected database role grants/metadata %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"role_1","project_ref":"alpha","name":"app_writer","login":true,"inherit":false,"bypass_rls":true,"connection_limit":25,"password_secret_handle":"********","member_of":["authenticated"],"schema_grants":{"public":"usage,select,insert"},"metadata":{"purpose":"app","api_key":"********"},"status":"configured","message":"database role declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/roles":
			_, _ = w.Write([]byte(`[{"id":"role_1","project_ref":"alpha","name":"app_writer","login":true,"inherit":false,"bypass_rls":true,"connection_limit":25,"password_secret_handle":"********","member_of":["authenticated"],"schema_grants":{"public":"usage,select,insert"},"metadata":{"purpose":"app","api_key":"********"},"status":"configured","message":"database role declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/database/roles/app_writer":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	inherit := false
	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	role, err := client.CreateProjectDatabaseRole(context.Background(), "alpha", ProjectDatabaseRoleInput{
		Name:                 "app_writer",
		Login:                true,
		Inherit:              &inherit,
		BypassRLS:            true,
		ConnectionLimit:      25,
		PasswordSecretHandle: "secret://projects/alpha/db/app-writer",
		MemberOf:             []string{"authenticated"},
		SchemaGrants:         map[string]string{"public": "usage,select,insert"},
		Metadata:             map[string]string{"purpose": "app", "api_key": "secret://projects/alpha/db-role-api"},
	})
	if err != nil {
		t.Fatalf("create project database role: %v", err)
	}
	if role.ID != "role_1" || role.ProjectRef != "alpha" || role.PasswordSecretHandle != "********" || role.Metadata["api_key"] != "********" || role.CreatedAt.IsZero() || role.UpdatedAt.IsZero() {
		t.Fatalf("unexpected database role %#v", role)
	}
	roles, err := client.ListProjectDatabaseRoles(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project database roles: %v", err)
	}
	if len(roles) != 1 || roles[0].Name != "app_writer" || roles[0].Inherit || !roles[0].BypassRLS {
		t.Fatalf("unexpected database roles %#v", roles)
	}
	if err := client.DeleteProjectDatabaseRole(context.Background(), "alpha", "app_writer"); err != nil {
		t.Fatalf("delete project database role: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/database/roles",
		"GET /v1/projects/alpha/database/roles",
		"DELETE /v1/projects/alpha/database/roles/app_writer",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectDatabaseExtensionRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/database/extensions":
			_, _ = w.Write([]byte(`[{"id":"default:pg_cron","project_ref":"alpha","name":"pg_cron","schema":"extensions","enabled":true,"status":"enabled","message":"enabled by Compose init SQL","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/projects/alpha/database/extensions/pg_cron":
			var got ProjectDatabaseExtensionInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if len(seen) == 2 {
				if got.Schema != "extensions" || got.Version != "1.6" || got.Enabled == nil || *got.Enabled {
					t.Fatalf("unexpected database extension payload %#v", got)
				}
				_, _ = w.Write([]byte(`{"id":"ext_1","project_ref":"alpha","name":"pg_cron","schema":"extensions","version":"1.6","enabled":false,"status":"disabled","message":"database extension disabled","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
				return
			}
			if got.Schema != "" || got.Version != "" || got.Enabled != nil {
				t.Fatalf("unexpected database extension reset payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"ext_2","project_ref":"alpha","name":"pg_cron","schema":"extensions","enabled":true,"status":"enabled","message":"database extension enabled","created_at":"2026-06-05T12:01:00Z","updated_at":"2026-06-05T12:01:00Z"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	extensions, err := client.ListProjectDatabaseExtensions(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project database extensions: %v", err)
	}
	if len(extensions) != 1 || extensions[0].Name != "pg_cron" || !extensions[0].Enabled {
		t.Fatalf("unexpected database extensions %#v", extensions)
	}
	enabled := false
	extension, err := client.UpdateProjectDatabaseExtension(context.Background(), "alpha", "pg_cron", ProjectDatabaseExtensionInput{
		Schema:  "extensions",
		Version: "1.6",
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("update project database extension: %v", err)
	}
	if extension.ID != "ext_1" || extension.ProjectRef != "alpha" || extension.Name != "pg_cron" || extension.Enabled || extension.Status != "disabled" {
		t.Fatalf("unexpected updated database extension %#v", extension)
	}
	reset, err := client.UpdateProjectDatabaseExtension(context.Background(), "alpha", "pg_cron", ProjectDatabaseExtensionInput{})
	if err != nil {
		t.Fatalf("reset project database extension: %v", err)
	}
	if reset.ID != "ext_2" || !reset.Enabled || reset.Version != "" {
		t.Fatalf("unexpected reset database extension %#v", reset)
	}

	expected := []string{
		"GET /v1/projects/alpha/database/extensions",
		"PUT /v1/projects/alpha/database/extensions/pg_cron",
		"PUT /v1/projects/alpha/database/extensions/pg_cron",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectEmbeddingJobLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/embeddings":
			var got ProjectEmbeddingJobInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "docs-embeddings" || got.SourceSchema != "public" || got.SourceTable != "documents" || got.SourceColumn != "body" {
				t.Fatalf("unexpected embedding job source payload %#v", got)
			}
			if got.PrimaryKeyColumn != "id" || got.DestinationTable != "document_embeddings" || got.DestinationColumn != "embedding" {
				t.Fatalf("unexpected embedding job destination payload %#v", got)
			}
			if got.Provider != "openai" || got.Model != "text-embedding-3-small" || got.Dimension != 1536 || got.Schedule != "manual" || got.BatchSize != 100 {
				t.Fatalf("unexpected embedding job runtime payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"emb_1","project_ref":"alpha","name":"docs-embeddings","source_schema":"public","source_table":"documents","source_column":"body","primary_key_column":"id","destination_table":"document_embeddings","destination_column":"embedding","provider":"openai","model":"text-embedding-3-small","dimension":1536,"schedule":"manual","batch_size":100,"status":"configured","message":"automatic embedding job declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/embeddings":
			_, _ = w.Write([]byte(`[{"id":"emb_1","project_ref":"alpha","name":"docs-embeddings","source_schema":"public","source_table":"documents","source_column":"body","primary_key_column":"id","destination_table":"document_embeddings","destination_column":"embedding","provider":"openai","model":"text-embedding-3-small","dimension":1536,"schedule":"manual","batch_size":100,"status":"configured","message":"automatic embedding job declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/embeddings/emb_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	job, err := client.CreateProjectEmbeddingJob(context.Background(), "alpha", ProjectEmbeddingJobInput{
		Name:              "docs-embeddings",
		SourceSchema:      "public",
		SourceTable:       "documents",
		SourceColumn:      "body",
		PrimaryKeyColumn:  "id",
		DestinationTable:  "document_embeddings",
		DestinationColumn: "embedding",
		Provider:          "openai",
		Model:             "text-embedding-3-small",
		Dimension:         1536,
		Schedule:          "manual",
		BatchSize:         100,
	})
	if err != nil {
		t.Fatalf("create project embedding job: %v", err)
	}
	if job.ID != "emb_1" || job.ProjectRef != "alpha" || job.Status != "configured" || job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() {
		t.Fatalf("unexpected embedding job %#v", job)
	}
	jobs, err := client.ListProjectEmbeddingJobs(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project embedding jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "emb_1" || jobs[0].Name != "docs-embeddings" || jobs[0].DestinationTable != "document_embeddings" {
		t.Fatalf("unexpected embedding jobs %#v", jobs)
	}
	if err := client.DeleteProjectEmbeddingJob(context.Background(), "alpha", "emb_1"); err != nil {
		t.Fatalf("delete project embedding job: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/embeddings",
		"GET /v1/projects/alpha/embeddings",
		"DELETE /v1/projects/alpha/embeddings/emb_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectFunctionLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/functions":
			var got ProjectFunctionInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.Name != "hello-api" || got.Entrypoint != "index.ts" || !got.VerifyJWT {
				t.Fatalf("unexpected function deploy metadata payload %#v", got)
			}
			if got.Source != "Deno.serve(() => new Response('ok'))" {
				t.Fatalf("unexpected function source payload %#v", got)
			}
			if got.Secrets["API_KEY"] != "secret://projects/alpha/functions/api-key" {
				t.Fatalf("unexpected function secrets payload %#v", got.Secrets)
			}
			_, _ = w.Write([]byte(`{"id":"fn_1","project_ref":"alpha","name":"hello-api","version":2,"entrypoint":"index.ts","verify_jwt":true,"status":"deployed","source_hash":"abc123","source_bytes":37,"secrets":{"API_KEY":"********"},"created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:30:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/functions":
			_, _ = w.Write([]byte(`[{"id":"fn_1","project_ref":"alpha","name":"hello-api","version":2,"entrypoint":"index.ts","verify_jwt":true,"status":"deployed","source_hash":"abc123","source_bytes":37,"secrets":{"API_KEY":"********"},"created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:30:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/functions/hello-api":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	function, err := client.DeployProjectFunction(context.Background(), "alpha", ProjectFunctionInput{
		Name:       "hello-api",
		Entrypoint: "index.ts",
		VerifyJWT:  true,
		Source:     "Deno.serve(() => new Response('ok'))",
		Secrets: map[string]string{
			"API_KEY": "secret://projects/alpha/functions/api-key",
		},
	})
	if err != nil {
		t.Fatalf("deploy project function: %v", err)
	}
	if function.ID != "fn_1" || function.ProjectRef != "alpha" || function.Version != 2 || function.SourceHash != "abc123" || function.CreatedAt.IsZero() || function.UpdatedAt.IsZero() {
		t.Fatalf("unexpected project function %#v", function)
	}
	if function.Secrets["API_KEY"] != "********" {
		t.Fatalf("expected masked function secret, got %#v", function.Secrets)
	}
	functions, err := client.ListProjectFunctions(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project functions: %v", err)
	}
	if len(functions) != 1 || functions[0].ID != "fn_1" || functions[0].Name != "hello-api" || functions[0].Version != 2 {
		t.Fatalf("unexpected project functions %#v", functions)
	}
	if err := client.DeleteProjectFunction(context.Background(), "alpha", "hello-api"); err != nil {
		t.Fatalf("delete project function: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/functions",
		"GET /v1/projects/alpha/functions",
		"DELETE /v1/projects/alpha/functions/hello-api",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectFunctionRegionLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/functions/regions":
			var got ProjectFunctionRegionInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.FunctionName != "hello-api" || got.HostID != "host-1" || got.Region != "us-east-1" || got.RoutingPolicy != "primary" {
				t.Fatalf("unexpected function region payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"region_1","project_ref":"alpha","function_name":"hello-api","host_id":"host-1","region":"us-east-1","routing_policy":"primary","invocation_url":"https://hello-api.us-east-1.alpha.functions.internal","status":"configured","message":"regional invocation declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/functions/regions":
			_, _ = w.Write([]byte(`[{"id":"region_1","project_ref":"alpha","function_name":"hello-api","host_id":"host-1","region":"us-east-1","routing_policy":"primary","invocation_url":"https://hello-api.us-east-1.alpha.functions.internal","status":"configured","message":"regional invocation declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/functions/regions/region_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	region, err := client.CreateProjectFunctionRegion(context.Background(), "alpha", ProjectFunctionRegionInput{
		FunctionName:  "hello-api",
		HostID:        "host-1",
		Region:        "us-east-1",
		RoutingPolicy: "primary",
	})
	if err != nil {
		t.Fatalf("create project function region: %v", err)
	}
	if region.ID != "region_1" || region.ProjectRef != "alpha" || region.InvocationURL == "" || region.CreatedAt.IsZero() || region.UpdatedAt.IsZero() {
		t.Fatalf("unexpected function region %#v", region)
	}
	regions, err := client.ListProjectFunctionRegions(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project function regions: %v", err)
	}
	if len(regions) != 1 || regions[0].ID != "region_1" || regions[0].FunctionName != "hello-api" || regions[0].RoutingPolicy != "primary" {
		t.Fatalf("unexpected function regions %#v", regions)
	}
	if err := client.DeleteProjectFunctionRegion(context.Background(), "alpha", "region_1"); err != nil {
		t.Fatalf("delete project function region: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/functions/regions",
		"GET /v1/projects/alpha/functions/regions",
		"DELETE /v1/projects/alpha/functions/regions/region_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientProjectFunctionStorageMountLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/alpha/functions/storage-mounts":
			var got ProjectFunctionStorageMountInput
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got.FunctionName != "hello-api" || got.BucketName != "assets" || got.MountPath != "/mnt/assets" {
				t.Fatalf("unexpected function storage mount target payload %#v", got)
			}
			if !got.ReadOnly || got.Prefix != "public" || got.EnvAlias != "ASSETS_MOUNT" {
				t.Fatalf("unexpected function storage mount options payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"mount_1","project_ref":"alpha","function_name":"hello-api","bucket_name":"assets","mount_path":"/mnt/assets","read_only":true,"prefix":"public","env_alias":"ASSETS_MOUNT","status":"configured","message":"function storage mount declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/alpha/functions/storage-mounts":
			_, _ = w.Write([]byte(`[{"id":"mount_1","project_ref":"alpha","function_name":"hello-api","bucket_name":"assets","mount_path":"/mnt/assets","read_only":true,"prefix":"public","env_alias":"ASSETS_MOUNT","status":"configured","message":"function storage mount declaration recorded","created_at":"2026-06-05T12:00:00Z","updated_at":"2026-06-05T12:00:00Z"}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/alpha/functions/storage-mounts/mount_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	mount, err := client.CreateProjectFunctionStorageMount(context.Background(), "alpha", ProjectFunctionStorageMountInput{
		FunctionName: "hello-api",
		BucketName:   "assets",
		MountPath:    "/mnt/assets",
		ReadOnly:     true,
		Prefix:       "public",
		EnvAlias:     "ASSETS_MOUNT",
	})
	if err != nil {
		t.Fatalf("create project function storage mount: %v", err)
	}
	if mount.ID != "mount_1" || mount.ProjectRef != "alpha" || !mount.ReadOnly || mount.CreatedAt.IsZero() || mount.UpdatedAt.IsZero() {
		t.Fatalf("unexpected function storage mount %#v", mount)
	}
	mounts, err := client.ListProjectFunctionStorageMounts(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("list project function storage mounts: %v", err)
	}
	if len(mounts) != 1 || mounts[0].ID != "mount_1" || mounts[0].BucketName != "assets" || mounts[0].EnvAlias != "ASSETS_MOUNT" {
		t.Fatalf("unexpected function storage mounts %#v", mounts)
	}
	if err := client.DeleteProjectFunctionStorageMount(context.Background(), "alpha", "mount_1"); err != nil {
		t.Fatalf("delete project function storage mount: %v", err)
	}

	expected := []string{
		"POST /v1/projects/alpha/functions/storage-mounts",
		"GET /v1/projects/alpha/functions/storage-mounts",
		"DELETE /v1/projects/alpha/functions/storage-mounts/mount_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}

func TestClientListOrgsAndNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"org_1","name":"Platform"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/missing":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	orgs, err := client.ListOrgs(context.Background())
	if err != nil {
		t.Fatalf("list orgs: %v", err)
	}
	if len(orgs) != 1 || orgs[0].ID != "org_1" || orgs[0].Name != "Platform" {
		t.Fatalf("unexpected orgs %#v", orgs)
	}
	_, err = client.GetProject(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestClientOrgLifecycleRequests(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orgs":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["name"] != "Platform" {
				t.Fatalf("unexpected org create payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"org_1","name":"Platform"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orgs/org_1":
			_, _ = w.Write([]byte(`{"id":"org_1","name":"Platform"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/orgs/org_1":
			var got map[string]string
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if got["name"] != "Renamed" {
				t.Fatalf("unexpected org update payload %#v", got)
			}
			_, _ = w.Write([]byte(`{"id":"org_1","name":"Renamed"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/orgs/org_1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	org, err := client.CreateOrg(context.Background(), "Platform")
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if org.ID != "org_1" || org.Name != "Platform" {
		t.Fatalf("unexpected org %#v", org)
	}
	if _, err := client.GetOrg(context.Background(), "org_1"); err != nil {
		t.Fatalf("get org: %v", err)
	}
	updated, err := client.UpdateOrg(context.Background(), "org_1", "Renamed")
	if err != nil {
		t.Fatalf("update org: %v", err)
	}
	if updated.Name != "Renamed" {
		t.Fatalf("unexpected updated org %#v", updated)
	}
	if err := client.DeleteOrg(context.Background(), "org_1"); err != nil {
		t.Fatalf("delete org: %v", err)
	}

	expected := []string{
		"POST /v1/orgs",
		"GET /v1/orgs/org_1",
		"PUT /v1/orgs/org_1",
		"DELETE /v1/orgs/org_1",
	}
	if strings.Join(seen, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("unexpected requests:\n%s", strings.Join(seen, "\n"))
	}
}
