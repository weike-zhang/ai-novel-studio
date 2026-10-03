package worlds

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("world not found")

type Sort string

const (
	SortNewest      Sort = "newest"
	SortPopular     Sort = "popular"
	SortRecommended Sort = "recommended"
)

type PublicWorldQuery struct {
	Keyword string
	Type    string
	Tags    []string
	Sort    Sort
	Cursor  string
	Limit   int
}

type WorldCard struct {
	ID              string    `json:"id"`
	OwnerID         string    `json:"ownerId"`
	AuthorNickname  string    `json:"authorNickname"`
	Title           string    `json:"title"`
	Summary         string    `json:"summary"`
	Hook            string    `json:"hook,omitempty"`
	Promise         string    `json:"promise,omitempty"`
	PlayTime        string    `json:"playTime,omitempty"`
	Format          string    `json:"format,omitempty"`
	FeaturedRank    int       `json:"featuredRank,omitempty"`
	Cover           string    `json:"cover,omitempty"`
	Type            string    `json:"type,omitempty"`
	Tags            []string  `json:"tags"`
	SimulationCount int64     `json:"simulationCount"`
	Version         int64     `json:"version"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type PublicWorldPage struct {
	Worlds     []WorldCard `json:"worlds"`
	NextCursor string      `json:"nextCursor,omitempty"`
}

type World struct {
	WorldCard
	Visibility string          `json:"visibility"`
	Status     string          `json:"status"`
	Setting    json.RawMessage `json:"setting"`
}

type CreateJourneyInput struct {
	UserID  string
	WorldID string
}

type Journey struct {
	ID            string          `json:"id"`
	UserID        string          `json:"userId"`
	WorldID       string          `json:"worldId"`
	WorldVersion  int64           `json:"worldVersion"`
	WorldSnapshot json.RawMessage `json:"worldSnapshot"`
	Status        string          `json:"status"`
	Version       int64           `json:"version"`
	CreatedAt     time.Time       `json:"createdAt"`
}
