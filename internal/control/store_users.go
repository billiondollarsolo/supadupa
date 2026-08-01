package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *MemoryStore) CreateUser(ctx context.Context, req CreateUserRequest) (User, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		return User{}, fmt.Errorf("email is required")
	}
	if err := validateUserPassword(req.Password, true); err != nil {
		return User{}, err
	}
	role := req.Role
	if role == "" {
		role = "admin"
	}
	user := User{
		ID:           newID(),
		Email:        email,
		PasswordHash: hashPassword(req.Password),
		Role:         role,
		TokenVersion: 1,
		CreatedAt:    time.Now().UTC(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[email]; ok {
		return User{}, fmt.Errorf("%w: user %s already exists", ErrConflict, email)
	}
	s.users[email] = user
	return user, nil
}

func (s *MemoryStore) UpdateUser(ctx context.Context, id string, req UpdateUserRequest) (User, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return User{}, fmt.Errorf("user id is required")
	}
	nextEmail := strings.ToLower(strings.TrimSpace(req.Email))
	if nextEmail == "" {
		return User{}, fmt.Errorf("email is required")
	}
	nextRole := strings.TrimSpace(req.Role)
	if nextRole == "" {
		nextRole = "member"
	}
	if err := validateUserPassword(req.Password, false); err != nil {
		return User{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var currentEmail string
	var user User
	for email, candidate := range s.users {
		if candidate.ID == id {
			currentEmail = email
			user = candidate
			break
		}
	}
	if currentEmail == "" {
		return User{}, fmt.Errorf("%w: user %s", ErrNotFound, id)
	}
	securityStateChanged := currentEmail != nextEmail || user.Role != nextRole || req.Password != ""
	if currentEmail != nextEmail {
		if _, ok := s.users[nextEmail]; ok {
			return User{}, fmt.Errorf("%w: user %s already exists", ErrConflict, nextEmail)
		}
		delete(s.users, currentEmail)
		for orgID, members := range s.memberships {
			if member, ok := members[currentEmail]; ok {
				delete(members, currentEmail)
				member.Email = nextEmail
				members[nextEmail] = member
				s.memberships[orgID] = members
			}
		}
		for teamID, members := range s.teamMembers {
			if member, ok := members[currentEmail]; ok {
				delete(members, currentEmail)
				member.Email = nextEmail
				members[nextEmail] = member
				s.teamMembers[teamID] = members
			}
		}
		for ref, grants := range s.projectAccess {
			for index, grant := range grants {
				if grant.SubjectType == "user" && grant.SubjectID == user.ID {
					grants[index].SubjectName = nextEmail
				}
			}
			s.projectAccess[ref] = grants
		}
	}
	user.Email = nextEmail
	user.Role = nextRole
	if req.Password != "" {
		user.PasswordHash = hashPassword(req.Password)
	}
	if securityStateChanged {
		user.TokenVersion = nextTokenVersion(user.TokenVersion)
	}
	s.users[nextEmail] = user
	return user, nil
}

func (s *MemoryStore) DeleteUser(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("user id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	var email string
	for candidateEmail, user := range s.users {
		if user.ID == id {
			email = candidateEmail
			break
		}
	}
	if email == "" {
		return fmt.Errorf("%w: user %s", ErrNotFound, id)
	}
	delete(s.users, email)
	for orgID, members := range s.memberships {
		delete(members, email)
		s.memberships[orgID] = members
	}
	for teamID, members := range s.teamMembers {
		delete(members, email)
		s.teamMembers[teamID] = members
	}
	for ref, grants := range s.projectAccess {
		filtered := grants[:0]
		for _, grant := range grants {
			if grant.SubjectType == "user" && grant.SubjectID == id {
				continue
			}
			filtered = append(filtered, grant)
		}
		s.projectAccess[ref] = append([]ProjectAccessGrant(nil), filtered...)
	}
	return nil
}

func (s *MemoryStore) ListUsers(ctx context.Context) ([]User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].Email < users[j].Email
	})
	return users, nil
}

func (s *MemoryStore) GetUserByID(ctx context.Context, id string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, user := range s.users {
		if user.ID == id {
			return user, nil
		}
	}
	return User{}, fmt.Errorf("%w: user %s", ErrNotFound, id)
}

func (s *MemoryStore) AuthenticateUser(ctx context.Context, email string, password string) (User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	s.mu.RLock()
	user, ok := s.users[email]
	s.mu.RUnlock()
	verified, needsRehash := verifyPasswordWithRehash(password, user.PasswordHash)
	if !ok || !verified {
		return User{}, fmt.Errorf("%w: invalid credentials", ErrNotFound)
	}
	if needsRehash {
		s.mu.Lock()
		if current, ok := s.users[email]; ok && current.PasswordHash == user.PasswordHash {
			user.PasswordHash = hashPassword(password)
			current.PasswordHash = user.PasswordHash
			s.users[email] = current
		}
		s.mu.Unlock()
	}
	return user, nil
}

func (s *MemoryStore) RecordUserLogin(ctx context.Context, userID string) (time.Time, error) {
	at := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return time.Time{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	user.LastLoginAt = &at
	s.users[user.Email] = user
	return at, nil
}

func (s *MemoryStore) VerifyUserMFA(ctx context.Context, userID string, code string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return User{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	if !user.MFAEnabled || user.MFASecret == "" {
		return User{}, fmt.Errorf("mfa is not enabled")
	}
	counter, ok := VerifyTOTPCodeCounter(user.MFASecret, code, time.Now().UTC())
	if !ok {
		return User{}, fmt.Errorf("invalid mfa code")
	}
	if int64(counter) <= user.MFALastCounter {
		return User{}, fmt.Errorf("mfa code has already been used")
	}
	user.MFALastCounter = int64(counter)
	user.MFAUpdatedAt = time.Now().UTC()
	s.users[user.Email] = user
	return user, nil
}

func (s *MemoryStore) GetUserMFAStatus(ctx context.Context, userID string) (MFAStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return MFAStatus{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	return mfaStatusForUser(user), nil
}

func (s *MemoryStore) BeginUserMFAEnrollment(ctx context.Context, userID string) (MFAEnrollment, error) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		return MFAEnrollment{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return MFAEnrollment{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	user.MFAPendingSecret = secret
	user.MFAUpdatedAt = time.Now().UTC()
	s.users[user.Email] = user

	return MFAEnrollment{
		MFAStatus:  mfaStatusForUser(user),
		Secret:     secret,
		OTPAuthURL: TOTPAuthURL("supadupa", user.Email, secret),
	}, nil
}

func (s *MemoryStore) ConfirmUserMFA(ctx context.Context, userID string, code string) (MFAStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return MFAStatus{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	if user.MFAPendingSecret == "" {
		return MFAStatus{}, fmt.Errorf("mfa enrollment is not pending")
	}
	counter, ok := VerifyTOTPCodeCounter(user.MFAPendingSecret, code, time.Now().UTC())
	if !ok {
		return MFAStatus{}, fmt.Errorf("invalid mfa code")
	}
	now := time.Now().UTC()
	user.MFASecret = user.MFAPendingSecret
	user.MFAPendingSecret = ""
	user.MFAEnabled = true
	user.MFAConfirmedAt = now
	user.MFAUpdatedAt = now
	user.MFALastCounter = int64(counter)
	user.TokenVersion = nextTokenVersion(user.TokenVersion)
	s.users[user.Email] = user
	return mfaStatusForUser(user), nil
}

func (s *MemoryStore) DisableUserMFA(ctx context.Context, userID string, code string) (MFAStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.userByIDLocked(userID)
	if !ok {
		return MFAStatus{}, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}
	if !user.MFAEnabled {
		user.MFAPendingSecret = ""
		user.MFAUpdatedAt = time.Now().UTC()
		user.TokenVersion = nextTokenVersion(user.TokenVersion)
		s.users[user.Email] = user
		return mfaStatusForUser(user), nil
	}
	counter, ok := VerifyTOTPCodeCounter(user.MFASecret, code, time.Now().UTC())
	if !ok {
		return MFAStatus{}, fmt.Errorf("invalid mfa code")
	}
	if int64(counter) <= user.MFALastCounter {
		return MFAStatus{}, fmt.Errorf("mfa code has already been used")
	}
	user.MFAEnabled = false
	user.MFASecret = ""
	user.MFAPendingSecret = ""
	user.MFAConfirmedAt = time.Time{}
	user.MFAUpdatedAt = time.Now().UTC()
	user.MFALastCounter = 0
	user.TokenVersion = nextTokenVersion(user.TokenVersion)
	s.users[user.Email] = user
	return mfaStatusForUser(user), nil
}

func nextTokenVersion(current int64) int64 {
	if current < 1 {
		return 1
	}
	return current + 1
}

func (s *MemoryStore) HasUsers(ctx context.Context) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) > 0
}
