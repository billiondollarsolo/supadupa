package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"supadupa2026/internal/control"
)

const (
	scimListResponseSchema = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimPatchOpSchema      = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	scimUserSchema         = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimGroupSchema        = "urn:ietf:params:scim:schemas:core:2.0:Group"
	scimUserExtension      = "urn:supadupa:params:scim:schemas:extension:User"
	scimGroupExtension     = "urn:supadupa:params:scim:schemas:extension:Group"
)

type scimListResponse struct {
	Schemas      []string `json:"schemas"`
	Total        int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    any      `json:"Resources"`
}

type scimMeta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created,omitempty"`
	Location     string    `json:"location,omitempty"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

type scimMember struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
}

type scimUserExtensionResource struct {
	Role string `json:"role,omitempty"`
}

type scimGroupExtensionResource struct {
	OrgID string `json:"org_id,omitempty"`
	Slug  string `json:"slug,omitempty"`
}

type scimUserResource struct {
	Schemas     []string                  `json:"schemas"`
	ID          string                    `json:"id"`
	ExternalID  string                    `json:"externalId,omitempty"`
	UserName    string                    `json:"userName"`
	DisplayName string                    `json:"displayName,omitempty"`
	Active      bool                      `json:"active"`
	Emails      []scimEmail               `json:"emails,omitempty"`
	Meta        scimMeta                  `json:"meta"`
	Extension   scimUserExtensionResource `json:"urn:supadupa:params:scim:schemas:extension:User,omitempty"`
}

type scimUserRequest struct {
	Schemas     []string                  `json:"schemas"`
	ExternalID  string                    `json:"externalId"`
	UserName    string                    `json:"userName"`
	DisplayName string                    `json:"displayName"`
	Active      *bool                     `json:"active"`
	Emails      []scimEmail               `json:"emails"`
	Password    string                    `json:"password"`
	Extension   scimUserExtensionResource `json:"urn:supadupa:params:scim:schemas:extension:User"`
}

type scimGroupResource struct {
	Schemas     []string                   `json:"schemas"`
	ID          string                     `json:"id"`
	ExternalID  string                     `json:"externalId,omitempty"`
	DisplayName string                     `json:"displayName"`
	Members     []scimMember               `json:"members,omitempty"`
	Meta        scimMeta                   `json:"meta"`
	Extension   scimGroupExtensionResource `json:"urn:supadupa:params:scim:schemas:extension:Group,omitempty"`
}

type scimGroupRequest struct {
	Schemas     []string                   `json:"schemas"`
	ExternalID  string                     `json:"externalId"`
	DisplayName string                     `json:"displayName"`
	Members     []scimMember               `json:"members"`
	Extension   scimGroupExtensionResource `json:"urn:supadupa:params:scim:schemas:extension:Group"`
}

type scimPatchRequest struct {
	Schemas    []string        `json:"schemas"`
	Operations []scimOperation `json:"Operations"`
}

type scimOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

func scimList[T any](resources []T) scimListResponse {
	if resources == nil {
		resources = []T{}
	}
	return scimListResponse{
		Schemas:      []string{scimListResponseSchema},
		Total:        len(resources),
		StartIndex:   1,
		ItemsPerPage: len(resources),
		Resources:    resources,
	}
}

func scimUserFromControl(r *http.Request, user control.User) scimUserResource {
	return scimUserResource{
		Schemas:     []string{scimUserSchema, scimUserExtension},
		ID:          user.ID,
		ExternalID:  user.ID,
		UserName:    user.Email,
		DisplayName: user.Email,
		Active:      true,
		Emails:      []scimEmail{{Value: user.Email, Primary: true}},
		Meta: scimMeta{
			ResourceType: "User",
			Created:      user.CreatedAt,
			Location:     absoluteResourceLocation(r, "/v1/scim/v2/Users/"+user.ID),
		},
		Extension: scimUserExtensionResource{Role: user.Role},
	}
}

func scimGroupFromControl(r *http.Request, store control.Store, team control.Team) (scimGroupResource, error) {
	members, err := store.ListTeamMembers(r.Context(), team.OrgID, team.Slug)
	if err != nil {
		return scimGroupResource{}, err
	}
	scimMembers := make([]scimMember, 0, len(members))
	for _, member := range members {
		scimMembers = append(scimMembers, scimMember{Value: member.UserID, Display: member.Email})
	}
	return scimGroupResource{
		Schemas:     []string{scimGroupSchema, scimGroupExtension},
		ID:          team.ID,
		ExternalID:  team.OrgID,
		DisplayName: team.Name,
		Members:     scimMembers,
		Meta: scimMeta{
			ResourceType: "Group",
			Created:      team.CreatedAt,
			Location:     absoluteResourceLocation(r, "/v1/scim/v2/Groups/"+team.ID),
		},
		Extension: scimGroupExtensionResource{OrgID: team.OrgID, Slug: team.Slug},
	}, nil
}

func scimEmailFromUserRequest(payload scimUserRequest) string {
	for _, email := range payload.Emails {
		if email.Primary && strings.TrimSpace(email.Value) != "" {
			return strings.ToLower(strings.TrimSpace(email.Value))
		}
	}
	for _, email := range payload.Emails {
		if strings.TrimSpace(email.Value) != "" {
			return strings.ToLower(strings.TrimSpace(email.Value))
		}
	}
	return strings.ToLower(strings.TrimSpace(payload.UserName))
}

func scimGroupOrgID(r *http.Request, payload scimGroupRequest) string {
	if orgID := strings.TrimSpace(payload.Extension.OrgID); orgID != "" {
		return orgID
	}
	if orgID := strings.TrimSpace(payload.ExternalID); orgID != "" {
		return orgID
	}
	return strings.TrimSpace(r.URL.Query().Get("org_id"))
}

func scimMemberEmail(ctx context.Context, store control.Store, member scimMember) (string, error) {
	value := strings.TrimSpace(member.Value)
	if value != "" {
		user, err := store.GetUserByID(ctx, value)
		if err == nil {
			return user.Email, nil
		}
		if strings.Contains(value, "@") {
			return strings.ToLower(value), nil
		}
		return "", err
	}
	display := strings.TrimSpace(member.Display)
	if strings.Contains(display, "@") {
		return strings.ToLower(display), nil
	}
	return "", fmt.Errorf("SCIM group member value or email display is required")
}

func findSCIMTeam(ctx context.Context, store control.Store, id string) (control.Team, error) {
	id = strings.TrimSpace(id)
	orgs, err := store.ListOrgs(ctx)
	if err != nil {
		return control.Team{}, err
	}
	for _, org := range orgs {
		teams, err := store.ListOrgTeams(ctx, org.ID)
		if err != nil {
			return control.Team{}, err
		}
		for _, team := range teams {
			if team.ID == id || team.Slug == id {
				return team, nil
			}
		}
	}
	return control.Team{}, fmt.Errorf("%w: SCIM group %s", control.ErrNotFound, id)
}

func scimBoolValue(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true":
			return true
		case "false":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}

func scimStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func scimPatchObjectValues(value any) (string, string, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", "", false
	}
	var email string
	var role string
	changed := false
	for key, raw := range object {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "username":
			email = strings.ToLower(strings.TrimSpace(scimStringValue(raw)))
			changed = true
		case strings.ToLower(scimUserExtension):
			extension, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if rawRole, ok := extension["role"]; ok {
				role = strings.TrimSpace(scimStringValue(rawRole))
				changed = true
			}
		}
	}
	return email, role, changed
}

func absoluteResourceLocation(r *http.Request, path string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.Split(forwarded, ",")[0]
	}
	return scheme + "://" + r.Host + path
}
