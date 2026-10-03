package worlds

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type pageCursor struct {
	Sort       Sort      `json:"sort"`
	Popularity int64     `json:"popularity,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
	ID         string    `json:"id"`
}

func buildPublicWorldQuery(input PublicWorldQuery) (string, []any, int, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 {
		return "", nil, 0, fmt.Errorf("limit must be between 1 and 100")
	}
	sort := input.Sort
	if sort == "" {
		sort = SortNewest
	}
	if sort != SortNewest && sort != SortPopular && sort != SortRecommended {
		return "", nil, 0, fmt.Errorf("unsupported sort %q", sort)
	}

	where := []string{"w.visibility='public'", "w.status='published'"}
	args := make([]any, 0, 8)
	if keyword := strings.TrimSpace(input.Keyword); keyword != "" {
		pattern := "%" + escapeLike(keyword) + "%"
		where = append(where, "(w.title LIKE ? ESCAPE '=' OR w.summary LIKE ? ESCAPE '=' OR u.nickname LIKE ? ESCAPE '=')")
		args = append(args, pattern, pattern, pattern)
	}
	if worldType := strings.TrimSpace(input.Type); worldType != "" {
		where = append(where, "(JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.type'))=? OR JSON_CONTAINS(COALESCE(JSON_EXTRACT(w.setting_json,'$.genre'),JSON_ARRAY()),JSON_QUOTE(?)))")
		args = append(args, worldType, worldType)
	}
	for _, rawTag := range input.Tags {
		if tag := strings.TrimSpace(rawTag); tag != "" {
			where = append(where, "JSON_CONTAINS(COALESCE(JSON_EXTRACT(w.setting_json,'$.tags'),JSON_EXTRACT(w.setting_json,'$.genre'),JSON_ARRAY()),JSON_QUOTE(?))")
			args = append(args, tag)
		}
	}

	const columns = `BIN_TO_UUID(w.id,1) AS id,BIN_TO_UUID(w.owner_id,1) AS owner_id,u.nickname AS author_nickname,w.title,w.summary,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.discovery.hook')),'') AS hook,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.discovery.promise')),'') AS promise,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.discovery.playTime')),'') AS play_time,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.discovery.format')),'') AS format,CAST(COALESCE(JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.discovery.featuredRank')),'0') AS UNSIGNED) AS featured_rank,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.cover')),'') AS cover,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.type')),JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.genre[0]')),'') AS world_type,COALESCE(JSON_EXTRACT(w.setting_json,'$.tags'),JSON_EXTRACT(w.setting_json,'$.genre'),JSON_ARRAY()) AS tags,(SELECT COUNT(*) FROM simulations s WHERE s.world_id=w.id) AS simulation_count,(SELECT COUNT(*) FROM simulations recent WHERE recent.world_id=w.id AND recent.created_at>=UTC_TIMESTAMP(6)-INTERVAL 30 DAY) AS popularity,w.version,w.updated_at,w.id AS raw_id`
	inner := "SELECT " + columns + " FROM worlds w JOIN users u ON u.id=w.owner_id WHERE " + strings.Join(where, " AND ")
	query := "SELECT id,owner_id,author_nickname,title,summary,hook,promise,play_time,format,featured_rank,cover,world_type,tags,simulation_count,popularity,version,updated_at,raw_id FROM (" + inner + ") discovered"

	if input.Cursor != "" {
		cursor, err := decodeCursor(input.Cursor)
		if err != nil {
			return "", nil, 0, err
		}
		if cursor.Sort != sort {
			return "", nil, 0, fmt.Errorf("cursor sort does not match request")
		}
		if sort == SortNewest {
			query += " WHERE (updated_at<? OR (updated_at=? AND raw_id<UUID_TO_BIN(?,1)))"
			args = append(args, cursor.UpdatedAt, cursor.UpdatedAt, cursor.ID)
		} else {
			query += " WHERE (popularity<? OR (popularity=? AND (updated_at<? OR (updated_at=? AND raw_id<UUID_TO_BIN(?,1)))))"
			args = append(args, cursor.Popularity, cursor.Popularity, cursor.UpdatedAt, cursor.UpdatedAt, cursor.ID)
		}
	}
	if sort == SortNewest {
		query += " ORDER BY updated_at DESC,raw_id DESC"
	} else {
		// P0 recommendation is deliberately deterministic until a separate recommendation policy exists.
		query += " ORDER BY popularity DESC,updated_at DESC,raw_id DESC"
	}
	query += " LIMIT ?"
	args = append(args, limit+1)
	return query, args, limit, nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, "=", "==")
	value = strings.ReplaceAll(value, "%", "=%")
	return strings.ReplaceAll(value, "_", "=_")
}

func encodeCursor(cursor pageCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCursor(value string) (pageCursor, error) {
	var cursor pageCursor
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor, fmt.Errorf("invalid cursor: %w", err)
	}
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return cursor, fmt.Errorf("invalid cursor: %w", err)
	}
	if !isUUID(cursor.ID) || cursor.UpdatedAt.IsZero() || cursor.Popularity < 0 {
		return cursor, fmt.Errorf("invalid cursor contents")
	}
	return cursor, nil
}
