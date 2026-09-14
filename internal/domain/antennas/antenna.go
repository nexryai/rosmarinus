package antennas

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Source string

const (
	SourceAll            Source = "all"
	SourceUsers          Source = "users"
	SourceUsersBlacklist Source = "users_blacklist"
)

const MaxPerActor = 5

type Antenna struct {
	ID              string
	OwnerAccountID  string
	OwnerActorID    string
	Name            string
	Source          Source
	Users           []string
	Keywords        [][]string
	ExcludeKeywords [][]string
	CaseSensitive   bool
	LocalOnly       bool
	ExcludeBots     bool
	WithReplies     bool
	WithFile        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Repository interface {
	Count(context.Context, string, string) (int64, error)
	Create(context.Context, Antenna) (*Antenna, error)
	Update(context.Context, Antenna) (*Antenna, error)
	Delete(context.Context, string, string, string) (bool, error)
}

func Normalize(antenna Antenna) (Antenna, error) {
	antenna.OwnerAccountID = strings.TrimSpace(antenna.OwnerAccountID)
	antenna.OwnerActorID = strings.TrimSpace(antenna.OwnerActorID)
	antenna.ID = strings.TrimSpace(antenna.ID)
	antenna.Name = strings.TrimSpace(antenna.Name)
	if antenna.OwnerAccountID == "" || antenna.OwnerActorID == "" {
		return Antenna{}, fmt.Errorf("antenna owner account and actor are required")
	}
	if antenna.Name == "" || len([]rune(antenna.Name)) > 100 {
		return Antenna{}, fmt.Errorf("antenna name must contain 1 to 100 characters")
	}
	switch antenna.Source {
	case SourceAll, SourceUsers, SourceUsersBlacklist:
	default:
		return Antenna{}, fmt.Errorf("antenna source must be all, users, or users_blacklist")
	}
	antenna.Users = normalizeStrings(antenna.Users, 100)
	if antenna.Source == SourceUsers && len(antenna.Users) == 0 {
		return Antenna{}, fmt.Errorf("users source requires at least one user")
	}
	antenna.Keywords = normalizeGroups(antenna.Keywords)
	antenna.ExcludeKeywords = normalizeGroups(antenna.ExcludeKeywords)
	if len(antenna.Keywords) == 0 && len(antenna.ExcludeKeywords) == 0 {
		return Antenna{}, fmt.Errorf("at least one include or exclude keyword is required")
	}
	return antenna, nil
}

func normalizeStrings(values []string, maximum int) []string {
	result := make([]string, 0, min(len(values), maximum))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > 1024 {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) == maximum {
			break
		}
	}
	return result
}

func normalizeGroups(groups [][]string) [][]string {
	result := make([][]string, 0, min(len(groups), 20))
	for _, group := range groups {
		words := normalizeStrings(group, 20)
		for i := range words {
			if len([]rune(words[i])) > 100 {
				words[i] = string([]rune(words[i])[:100])
			}
		}
		if len(words) > 0 {
			result = append(result, words)
		}
		if len(result) == 20 {
			break
		}
	}
	return result
}
