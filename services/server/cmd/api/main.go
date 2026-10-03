package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/achievements"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/auth"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/benefits"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/media"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/openaiutil"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/reports"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/turnjobs"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/worlddrafts"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/worlds"
	"github.com/redis/go-redis/v9"
)

const developmentUserHeader = "X-Test-User-ID"

// Cover generation price in credits.
const coverCredits = 8

// User-uploaded cover validation: homepage covers are rendered in 16:9 and the
// client re-encodes uploads to 1280x720, so the server enforces the same
// constraints defensively.
const (
	maxCoverUploadBytes = 8 << 20
	minCoverWidth       = 960
	minCoverHeight      = 540
	maxCoverRatioDrift  = 0.04
)

var (
	errCoverUploadTooLarge    = errors.New("cover upload too large")
	errCoverUploadUnsupported = errors.New("cover upload type unsupported")
	errCoverUploadInvalid     = errors.New("cover upload not a decodable image")
	errCoverUploadTooSmall    = errors.New("cover upload too small")
	errCoverUploadRatio       = errors.New("cover upload not 16:9")
)

// Publish reward package.
const (
	publishRewardCredits = 90
	publishRewardCovers  = 3
)

func main() {
	if os.Getenv("APP_ENV") != "development" {
		gin.SetMode(gin.ReleaseMode)
	}
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		panic("MYSQL_DSN is required")
	}
	db, err := turnjobs.Open(dsn)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	ctx, cancel := requestContext()
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		panic("connect mysql: " + err.Error())
	}
	store := turnjobs.NewStore(db)
	benefitsStore := benefits.NewStore(db)
	achievementStore := achievements.NewStore(db)
	// The turn reservation is the maximum a single turn can cost; the actual
	// charge is settled by narrative length and the difference is refunded.
	turnReservation := int64(turnjobs.MaxTurnCredits)
	if value := os.Getenv("TEXT_CREDITS_BASE"); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil && parsed > 0 {
			turnReservation = parsed
		}
	}
	worldStore := worlds.NewStoreWithPublicationModerator(db, publicationModerator())
	worldHandler := worlds.NewHandler(worldStore)
	authoringHandler := worlds.NewAuthoringHandler(worldStore)
	worldDraftStore := worlddrafts.NewStore(db)
	worldDraftHandler := worlddrafts.NewHandler(worldDraftStore)
	coverGenerator, err := coverGeneratorFromEnv()
	if err != nil {
		panic(err)
	}
	assistGenerator, err := assistGeneratorFromEnv()
	if err != nil {
		panic(err)
	}
	mediaClient, err := mediaClientFromEnv()
	if err != nil {
		panic(err)
	}
	coverWorker := newCoverWorker(worldStore, coverGenerator, mediaClient)
	reportStore := reports.NewStore(db)
	reportHandler := reports.NewHandler(reportStore)
	authRepository, err := auth.NewMySQLRepository(db)
	if err != nil {
		panic(err)
	}
	passwords, err := auth.NewPasswordHasher(auth.DefaultArgon2idParams())
	if err != nil {
		panic(err)
	}
	sessionKey, recoveryKey := authenticationKeys()
	secrets, err := auth.NewSecretManager(sessionKey, recoveryKey)
	if err != nil {
		panic("configure authentication secrets: " + err.Error())
	}
	authService, err := auth.NewService(authRepository, passwords, secrets, auth.DefaultConfig())
	if err != nil {
		panic(err)
	}
	if err := wireRegistration(authService); err != nil {
		panic(err)
	}
	httpConfig := auth.DefaultHTTPConfig(os.Getenv("APP_ENV") != "development")
	if value := os.Getenv("SESSION_COOKIE_SECURE"); value == "true" {
		httpConfig.Production = true
	} else if value == "false" {
		httpConfig.Production = false
	}
	if value := os.Getenv("SESSION_COOKIE_LIFETIME_HOURS"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			httpConfig.SessionLifetime = time.Duration(parsed) * time.Hour
		}
	}
	authHandler, err := auth.NewHTTPHandler(authService, httpConfig)
	if err != nil {
		panic(err)
	}
	if os.Getenv("APP_ENV") == "development" {
		authHandler.EnableDevelopmentVerificationCodeEcho()
	}
	router := gin.New()
	router.Use(gin.Recovery())
	if err := router.SetTrustedProxies(nil); err != nil {
		panic("configure trusted proxies: " + err.Error())
	}
	router.GET("/healthz", func(c *gin.Context) {
		ctx, cancel := requestContext()
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	v1 := router.Group("/v1")
	v1.POST("/auth/login", gin.WrapH(authHandler))
	v1.POST("/auth/logout", gin.WrapH(authHandler))
	v1.POST("/auth/logout-all", gin.WrapH(authHandler))
	v1.POST("/auth/password/recover", gin.WrapH(authHandler))
	v1.POST("/auth/password/change", gin.WrapH(authHandler))
	v1.POST("/auth/recovery-code/rotate", gin.WrapH(authHandler))
	v1.POST("/auth/email-verification-codes", gin.WrapH(authHandler))
	v1.POST("/auth/register", gin.WrapH(authHandler))
	v1.GET("/me", gin.WrapH(authHandler))
	v1.GET("/me/sessions", gin.WrapH(authHandler))
	v1.POST("/me/sessions/revoke", gin.WrapH(authHandler))
	v1.GET("/worlds", gin.WrapH(worldHandler))
	v1.GET("/worlds/:id", sessionOrDevelopmentIdentity(authHandler, false), worldContextIdentity(), gin.WrapH(worldHandler))
	protected := v1.Group("")
	protected.Use(sessionOrDevelopmentIdentity(authHandler, true), worldContextIdentity())
	protected.POST("/simulations", gin.WrapH(worldHandler))
	protected.POST("/worlds", gin.WrapH(authoringHandler))
	protected.PATCH("/worlds/:id", gin.WrapH(authoringHandler))
	protected.POST("/worlds/:id/confirm-ai-fields", gin.WrapH(authoringHandler))
	protected.POST("/worlds/:id/publish", publishWorldHandler(authoringHandler, benefitsStore, coverWorker))
	protected.POST("/worlds/:id/unpublish", gin.WrapH(authoringHandler))
	protected.POST("/worlds/:id/cover", func(c *gin.Context) {
		cover, err := generateWorldCover(c, worldStore, coverGenerator, mediaClient, benefitsStore)
		if err != nil {
			log.Printf("generate cover for world %s: %v", c.Param("id"), err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "封面生成失败，请稍后重试"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"cover": cover})
	})
	protected.POST("/worlds/:id/cover/upload", func(c *gin.Context) {
		if err := uploadWorldCover(c, worldStore, mediaClient); err != nil {
			log.Printf("upload cover for world %s: %v", c.Param("id"), err)
			switch {
			case errors.Is(err, errCoverUploadTooLarge):
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "cover_too_large", "message": "封面图片不能超过 8MB"})
			case errors.Is(err, errCoverUploadUnsupported):
				c.JSON(http.StatusUnsupportedMediaType, gin.H{"code": "cover_unsupported_type", "message": "封面只支持 JPG、PNG 图片"})
			case errors.Is(err, errCoverUploadTooSmall):
				c.JSON(http.StatusBadRequest, gin.H{"code": "cover_too_small", "message": "封面图片清晰度不足，宽高至少 1280×720 像素"})
			case errors.Is(err, errCoverUploadRatio):
				c.JSON(http.StatusBadRequest, gin.H{"code": "cover_wrong_ratio", "message": "封面图片比例需接近 16:9"})
			case errors.Is(err, errCoverUploadInvalid):
				c.JSON(http.StatusBadRequest, gin.H{"code": "cover_invalid_image", "message": "封面图片无法读取，请换一张再试"})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "封面上传失败，请稍后重试"})
			}
			return
		}
	})
	protected.POST("/worlds/:id/reports", gin.WrapH(reportHandler))
	admin := protected.Group("")
	admin.Use(requireAdmin(db, adminAccountNames()))
	admin.POST("/worlds/:id/suspend", func(c *gin.Context) {
		world, err := worldStore.SuspendByModerator(c.Request.Context(), c.Param("id"))
		if err != nil {
			if errors.Is(err, worlds.ErrNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "世界不存在或已下架"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "下架失败，请稍后重试"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"world": world})
	})
	admin.POST("/worlds/:id/restore", func(c *gin.Context) {
		world, err := worldStore.RestoreByModerator(c.Request.Context(), c.Param("id"))
		if err != nil {
			if errors.Is(err, worlds.ErrNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "世界不存在或未处于下架状态"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "恢复失败，请稍后重试"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"world": world})
	})
	admin.POST("/worlds/:id/cover/generate", func(c *gin.Context) {
		coverWorker.Generate(c.Param("id"))
		c.JSON(http.StatusAccepted, gin.H{"queued": true})
	})
	admin.POST("/worlds/covers/regenerate-missing", func(c *gin.Context) {
		total, err := coverWorker.GenerateMissing(c.Request.Context())
		if err != nil {
			log.Printf("admin regenerate missing covers: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "无法列出待生成封面，请稍后重试"})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"queued": total})
	})
	admin.POST("/worlds/:id/featured", func(c *gin.Context) {
		rank, err := worldStore.SetFeaturedByModerator(c.Request.Context(), c.Param("id"))
		if err != nil {
			if errors.Is(err, worlds.ErrNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "世界不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "设置推荐失败，请稍后重试"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"featured": true, "rank": rank})
	})
	admin.POST("/worlds/:id/featured/remove", func(c *gin.Context) {
		if err := worldStore.UnsetFeaturedByModerator(c.Request.Context(), c.Param("id")); err != nil {
			if errors.Is(err, worlds.ErrNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"code": "not_found", "message": "世界不存在"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "取消推荐失败，请稍后重试"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"featured": false, "rank": 0})
	})
	protected.POST("/worlds/generate-draft", gin.WrapH(worldDraftHandler))
	protected.POST("/worlds/ai-assist", func(c *gin.Context) {
		var body struct {
			Action      string `json:"action" binding:"required"`
			Field       string `json:"field" binding:"required"`
			Current     string `json:"current"`
			ContextHint string `json:"contextHint"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			log.Printf("ai-assist bind error: %v body=%s", err, readBodyPreview(c))
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "请求体格式不正确"})
			return
		}
		result, err := assistGenerator.Generate(c.Request.Context(), worlddrafts.AssistRequest{
			Action:      worlddrafts.AssistAction(body.Action),
			Field:       body.Field,
			Current:     body.Current,
			ContextHint: body.ContextHint,
		})
		if err != nil {
			log.Printf("ai-assist generate error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "AI 暂时没有回应，请稍后重试"})
			return
		}
		c.JSON(http.StatusOK, result)
	})
	protected.POST("/me/checkin", func(c *gin.Context) {
		result, err := benefitsStore.CheckIn(c.Request.Context(), currentUser(c), time.Now())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "签到失败，请稍后重试"})
			return
		}
		c.JSON(http.StatusOK, result)
	})
	protected.GET("/me/benefits", func(c *gin.Context) {
		balance, err := benefitsStore.Balance(c.Request.Context(), currentUser(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "服务暂时不可用"})
			return
		}
		c.JSON(http.StatusOK, balance)
	})
	protected.GET("/me/checkins", func(c *gin.Context) {
		records, err := benefitsStore.CheckInHistory(c.Request.Context(), currentUser(c), 60)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "服务暂时不可用"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"checkins": records})
	})
	protected.GET("/me/simulations", func(c *gin.Context) {
		simulations, err := store.ListSimulations(c.Request.Context(), currentUser(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "服务暂时不可用"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"simulations": simulations})
	})
	protected.GET("/me/is-admin", func(c *gin.Context) {
		isAdmin := false
		for _, name := range adminAccountNames() {
			var accountName string
			if err := db.QueryRowContext(c.Request.Context(), `SELECT account_name_normalized FROM users WHERE id=UUID_TO_BIN(?,1) AND status='active'`, currentUser(c)).Scan(&accountName); err == nil && accountName == name {
				isAdmin = true
				break
			}
		}
		c.JSON(http.StatusOK, gin.H{"isAdmin": isAdmin})
	})
	protected.GET("/me/achievements", func(c *gin.Context) {
		claims, err := achievementStore.ListClaims(c.Request.Context(), currentUser(c), 200)
		if err != nil {
			log.Printf("list achievements for user %s: %v", currentUser(c), err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "成就暂时无法读取"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"achievements": claims})
	})
	protected.GET("/me/worlds", func(c *gin.Context) {
		worlds, err := worldStore.ListMyWorlds(c.Request.Context(), currentUser(c))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "internal_error", "message": "服务暂时不可用"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"worlds": worlds})
	})
	protected.GET("/simulations/:id", func(c *gin.Context) {
		snapshot, err := store.GetSimulationSnapshot(c.Request.Context(), currentUser(c), c.Param("id"))
		if err != nil {
			writeJobError(c, err)
			return
		}
		c.JSON(http.StatusOK, snapshot)
	})
	protected.POST("/simulations/:id/turn-jobs", func(c *gin.Context) {
		var body struct {
			Action      string `json:"action"`
			Interaction string `json:"interaction"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			log.Printf("create turn job bind error: %v body=%s", err, readBodyPreview(c))
			c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_request", "message": "请写下这一幕的行动"})
			return
		}
		if !turnjobs.ValidInteraction(body.Interaction) {
			body.Interaction = turnjobs.InteractionAct
		}
		job, fresh, err := store.Create(c.Request.Context(), turnjobs.CreateInput{
			UserID: currentUser(c), SimulationID: c.Param("id"), IdempotencyKey: c.GetHeader("Idempotency-Key"), Action: body.Action, Interaction: body.Interaction, Credits: turnReservation,
		})
		if err != nil {
			log.Printf("create turn job for simulation %s: %v", c.Param("id"), err)
			writeJobError(c, err)
			return
		}
		if !fresh {
			c.Header("Idempotent-Replayed", "true")
		}
		c.JSON(http.StatusAccepted, job)
	})
	protected.GET("/jobs/:id", func(c *gin.Context) {
		job, err := store.Get(c.Request.Context(), currentUser(c), c.Param("id"))
		if err != nil {
			if errors.Is(err, turnjobs.ErrNotFound) {
				worldDraftHandler.ServeHTTP(c.Writer, c.Request)
				return
			}
			writeJobError(c, err)
			return
		}
		c.JSON(http.StatusOK, job)
	})
	protected.POST("/jobs/:id/cancel", func(c *gin.Context) {
		if err := store.Cancel(c.Request.Context(), currentUser(c), c.Param("id")); err != nil {
			if errors.Is(err, turnjobs.ErrNotFound) {
				worldDraftHandler.ServeHTTP(c.Writer, c.Request)
				return
			}
			writeJobError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"status": "cancelled"})
	})
	protected.POST("/jobs/:id/retry", func(c *gin.Context) {
		job, err := store.Retry(c.Request.Context(), currentUser(c), c.Param("id"))
		if err != nil {
			if errors.Is(err, turnjobs.ErrNotFound) {
				worldDraftHandler.ServeHTTP(c.Writer, c.Request)
				return
			}
			writeJobError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, job)
	})
	protected.GET("/jobs/:id/events", func(c *gin.Context) {
		if _, err := store.GetForProcessing(c.Request.Context(), c.Param("id")); err != nil {
			if errors.Is(err, turnjobs.ErrNotFound) {
				worldDraftHandler.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		streamJob(c, store)
	})
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if err := router.Run(addr); err != nil {
		panic("api stopped: " + err.Error())
	}
}

func publicationModerator() worlds.PublicationModerator {
	provider := os.Getenv("MODERATION_PROVIDER")
	if os.Getenv("APP_ENV") == "development" && (provider == "" || provider == "synthetic") {
		return worlds.SyntheticAllowPublicationModerator{}
	}
	return worlds.NewRuleModerator()
}

func publishWorldHandler(inner http.Handler, benefitsStore *benefits.Store, coverWorker *coverWorker) gin.HandlerFunc {
	return func(c *gin.Context) {
		recorder := httptest.NewRecorder()
		inner.ServeHTTP(recorder, c.Request)
		for key, values := range recorder.Header() {
			for _, value := range values {
				c.Writer.Header().Add(key, value)
			}
		}
		c.Writer.WriteHeader(recorder.Code)
		_, _ = c.Writer.Write(recorder.Body.Bytes())
		if recorder.Code == http.StatusOK && c.Request.Method == http.MethodPost {
			var world struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &world); err == nil && world.Status == "published" {
				if userID := currentUser(c); userID != "" {
					now := time.Now()
					if err := benefitsStore.PublishReward(c.Request.Context(), userID, c.Param("id"), publishRewardCredits, publishRewardCovers, now); err != nil {
						log.Printf("publish reward for world %s: %v", c.Param("id"), err)
					}
				}
				// Newly published worlds get a cover generated in the background so
				// they appear with artwork on the discovery shelf without blocking.
				var published struct {
					Setting json.RawMessage `json:"setting"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &published); err == nil && settingCoverEmpty(published.Setting) {
					coverWorker.Generate(c.Param("id"))
				}
			}
		}
	}
}

// settingCoverEmpty reports whether a world setting carries no cover URL yet.
func settingCoverEmpty(setting json.RawMessage) bool {
	var value map[string]any
	if err := json.Unmarshal(setting, &value); err != nil {
		return false
	}
	cover, _ := value["cover"].(string)
	return cover == ""
}

func modelEndpoint(suffix string) *openaiutil.Endpoint {
	baseURL := strings.TrimSpace(os.Getenv("TEST_MODEL_BASE_URL" + suffix))
	apiKey := strings.TrimSpace(os.Getenv("TEST_MODEL_API_KEY" + suffix))
	model := strings.TrimSpace(os.Getenv("TEST_MODEL_NAME" + suffix))
	if baseURL == "" || apiKey == "" {
		return nil
	}
	if model == "" {
		model = "gpt-5-6-instant"
	}
	return &openaiutil.Endpoint{BaseURL: baseURL, APIKey: apiKey, Model: model}
}

func testModelName() string {
	if name := strings.TrimSpace(os.Getenv("TEST_MODEL_NAME")); name != "" {
		return name
	}
	return "gpt-5-6-instant"
}

func coverEndpoint(suffix string) *openaiutil.Endpoint {
	baseURL := strings.TrimSpace(os.Getenv("COVER_BASE_URL" + suffix))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("TEST_MODEL_BASE_URL" + suffix))
	}
	apiKey := strings.TrimSpace(os.Getenv("COVER_API_KEY" + suffix))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("TEST_MODEL_API_KEY" + suffix))
	}
	if baseURL == "" || apiKey == "" {
		return nil
	}
	model := strings.TrimSpace(os.Getenv("COVER_MODEL" + suffix))
	if model == "" {
		model = "gpt-image-2"
	}
	return &openaiutil.Endpoint{BaseURL: baseURL, APIKey: apiKey, Model: model}
}

func coverGeneratorFromEnv() (worlddrafts.CoverGenerator, error) {
	model := os.Getenv("COVER_MODEL")
	if model == "" {
		model = "gpt-image-2"
	}
	client := openaiutil.NewFailoverClient(coverEndpoint(""), coverEndpoint("FALLBACK"), 300*time.Second)
	return worlddrafts.NewOpenAICompatibleCoverGeneratorWithClient(model, client)
}

func assistGeneratorFromEnv() (*worlddrafts.AssistGenerator, error) {
	model := testModelName()
	client := openaiutil.NewFailoverClient(modelEndpoint(""), modelEndpoint("FALLBACK"), 20*time.Second)
	return worlddrafts.NewOpenAICompatibleAssistGeneratorWithClient(model, client)
}

func mediaClientFromEnv() (*media.Client, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	publicURL := os.Getenv("MINIO_PUBLIC_URL")
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	bucket := os.Getenv("MINIO_BUCKET")
	if bucket == "" {
		bucket = "novelstudio"
	}
	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("minio configuration is incomplete")
	}
	if isLoopbackEndpoint(endpoint) {
		return nil, fmt.Errorf("local MinIO is not allowed; configure your own external MinIO endpoint")
	}
	client, err := media.Open(media.Config{
		Endpoint:  endpoint,
		PublicURL: publicURL,
		AccessKey: accessKey,
		SecretKey: secretKey,
		Bucket:    bucket,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

// isLoopbackEndpoint reports whether an endpoint host is localhost or a
// loopback IP, which is rejected to enforce "server MinIO only".
func isLoopbackEndpoint(endpoint string) bool {
	host := strings.TrimSpace(endpoint)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

func generateWorldCover(c *gin.Context, worldStore *worlds.Store, generator worlddrafts.CoverGenerator, mediaClient *media.Client, benefitsStore *benefits.Store) (string, error) {
	userID := currentUser(c)
	if userID == "" {
		return "", errors.New("unauthenticated")
	}
	// Charge: prefer a free cover allowance, otherwise 8 credits.
	free, err := benefitsStore.ConsumeCoverAllowance(c.Request.Context(), userID)
	if err != nil {
		return "", err
	}
	if !free {
		if err := benefitsStore.ChargeCredits(c.Request.Context(), userID, "cover", c.Param("id"), coverCredits); err != nil {
			return "", err
		}
	}
	world, err := worldStore.Get(c.Request.Context(), c.Param("id"), userID)
	if err != nil {
		refundCoverCharge(c, benefitsStore, userID, free)
		return "", err
	}
	var setting map[string]any
	if err := json.Unmarshal(world.Setting, &setting); err != nil {
		refundCoverCharge(c, benefitsStore, userID, free)
		return "", fmt.Errorf("decode world setting: %w", err)
	}
	title := world.Title
	summary := world.Summary
	genre := ""
	if value, ok := setting["type"].(string); ok {
		genre = value
	}
	data, err := generator.Generate(c.Request.Context(), title, summary, genre)
	if err != nil {
		refundCoverCharge(c, benefitsStore, userID, free)
		return "", err
	}
	if err := mediaClient.EnsureBucket(c.Request.Context()); err != nil {
		refundCoverCharge(c, benefitsStore, userID, free)
		return "", err
	}
	url, err := worlddrafts.SaveCover(c.Request.Context(), mediaClient, world.ID, data)
	if err != nil {
		refundCoverCharge(c, benefitsStore, userID, free)
		return "", err
	}
	if err := worldStore.SetCover(c.Request.Context(), userID, world.ID, url); err != nil {
		refundCoverCharge(c, benefitsStore, userID, free)
		return "", err
	}
	return url, nil
}

// uploadWorldCover accepts a user-uploaded cover for a private draft world,
// validates type, size, resolution and aspect ratio, then stores it in media
// and points the world's cover at the resulting URL. Unlike AI generation,
// uploads are free and always overwrite the previous cover object.
func uploadWorldCover(c *gin.Context, worldStore *worlds.Store, mediaClient *media.Client) error {
	userID := currentUser(c)
	if userID == "" {
		return errors.New("unauthenticated")
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxCoverUploadBytes+1))
	if err != nil {
		return fmt.Errorf("read cover upload: %w", err)
	}
	if len(body) > maxCoverUploadBytes {
		return errCoverUploadTooLarge
	}
	contentType := c.GetHeader("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(body)
	}
	if contentType != "image/jpeg" && contentType != "image/png" {
		return errCoverUploadUnsupported
	}
	img, format, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return errCoverUploadInvalid
	}
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width < minCoverWidth || height < minCoverHeight {
		return errCoverUploadTooSmall
	}
	if ratio := float64(width) / float64(height); math.Abs(ratio-16.0/9.0) > maxCoverRatioDrift {
		return errCoverUploadRatio
	}
	if err := mediaClient.EnsureBucket(c.Request.Context()); err != nil {
		return fmt.Errorf("ensure media bucket: %w", err)
	}
	mime := "image/jpeg"
	if format == "png" {
		mime = "image/png"
	}
	url, err := mediaClient.Upload(c.Request.Context(), "covers/"+c.Param("id")+".png", body, mime)
	if err != nil {
		return fmt.Errorf("store cover upload: %w", err)
	}
	if err := worldStore.SetCover(c.Request.Context(), userID, c.Param("id"), url); err != nil {
		return fmt.Errorf("set uploaded cover: %w", err)
	}
	c.JSON(http.StatusOK, gin.H{"cover": url})
	return nil
}

// refundCoverCharge reverses a cover charge when generation fails. free
// indicates whether a free cover allowance was consumed instead of credits.
func refundCoverCharge(c *gin.Context, store *benefits.Store, userID string, free bool) {
	ctx := c.Request.Context()
	if free {
		if err := store.RefundCoverAllowance(ctx, userID); err != nil {
			log.Printf("refund cover allowance for user %s: %v", userID, err)
		}
		return
	}
	if err := store.RefundCredits(ctx, userID, "cover", c.Param("id"), coverCredits); err != nil {
		log.Printf("refund cover credits for user %s: %v", userID, err)
	}
}

// wireRegistration connects the registration flow's providers and Redis code
// store into the auth service. Development uses synthetic providers so the
// slice stays runnable locally; production requires a real email provider and
// a Turnstile secret key.
func wireRegistration(authService *auth.Service) error {
	production := os.Getenv("APP_ENV") != "development"

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:6379"
	}
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr, Password: os.Getenv("REDIS_PASSWORD")})
	codeStore, err := auth.NewRedisVerificationCodeStore(redisClient, auth.DefaultVerificationCodeStoreConfig())
	if err != nil {
		return err
	}

	sessionKey := []byte(os.Getenv("AUTH_SESSION_DIGEST_KEY"))
	if production && len(sessionKey) < 32 {
		return fmt.Errorf("AUTH_SESSION_DIGEST_KEY must contain at least 32 bytes in production")
	}
	if len(sessionKey) < 32 {
		devSession := sha256.Sum256([]byte("novelstudio-development-session-digest-key"))
		sessionKey = devSession[:]
	}
	emailCipher, err := auth.NewEmailCipher(sessionKey)
	if err != nil {
		return err
	}

	var emailProvider auth.EmailProvider
	var humanCheck auth.HumanCheckProvider
	if production {
		smtpAddr := strings.TrimSpace(os.Getenv("SMTP_ADDR"))
		if smtpAddr == "" {
			return fmt.Errorf("SMTP_ADDR is required in production")
		}
		smtpProvider, err := auth.NewSMTPEmailProvider(
			smtpAddr,
			os.Getenv("SMTP_USERNAME"),
			os.Getenv("SMTP_PASSWORD"),
			os.Getenv("SMTP_FROM"),
		)
		if err != nil {
			return err
		}
		emailProvider = smtpProvider
		turnstileKey := strings.TrimSpace(os.Getenv("TURNSTILE_SECRET_KEY"))
		if turnstileKey == "" {
			// Turnstile is not configured yet. Fall back to an accept-all
			// provider so registration stays reachable during bring-up; the
			// operator is warned loudly that this is not production-grade.
			log.Println("WARNING: TURNSTILE_SECRET_KEY is empty; human check is temporarily disabled")
			humanCheck = &auth.SyntheticHumanCheckProvider{DevToken: os.Getenv("TURNSTILE_DEV_TOKEN"), Action: "register"}
		} else {
			turnstile, err := auth.NewTurnstileProvider(turnstileKey, "register")
			if err != nil {
				return err
			}
			humanCheck = turnstile
		}
	} else {
		emailProvider = &auth.SyntheticEmailProvider{Log: os.Stderr}
		humanCheck = &auth.SyntheticHumanCheckProvider{DevToken: os.Getenv("TURNSTILE_DEV_TOKEN"), Action: "register"}
	}

	risk := &auth.DefaultRegistrationRiskService{MaxAccountsPerCookie: 3, MaxAccountsPerIP: 5}
	registrationConfig := auth.DefaultRegistrationConfig()
	if value := os.Getenv("REGISTRATION_INITIAL_LIGHT"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("REGISTRATION_INITIAL_LIGHT must be an integer")
		}
		registrationConfig.InitialLight = parsed
	}
	if value := os.Getenv("INITIAL_COVER_ALLOWANCE"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("INITIAL_COVER_ALLOWANCE must be an integer")
		}
		registrationConfig.InitialCoverAllowance = parsed
	}
	return authService.RegisterDependencies(emailCipher, codeStore, emailProvider, humanCheck, risk, registrationConfig)
}

func sessionOrDevelopmentIdentity(handler *auth.HTTPHandler, required bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if os.Getenv("APP_ENV") == "development" {
			if userID := c.GetHeader(developmentUserHeader); userID != "" {
				c.Set("user_id", userID)
				c.Next()
				return
			}
		}
		if _, err := c.Request.Cookie("novelstudio_session"); err != nil && !required {
			c.Next()
			return
		}
		authenticated, ok := handler.AuthenticateRequest(c.Writer, c.Request)
		if !ok {
			c.Abort()
			return
		}
		c.Request = authenticated
		if userID, ok := auth.UserIDFromContext(authenticated.Context()); ok {
			c.Set("user_id", userID)
		}
		c.Next()
	}
}

func worldContextIdentity() gin.HandlerFunc {
	return func(c *gin.Context) {
		if userID := currentUser(c); userID != "" {
			c.Request = c.Request.WithContext(worlds.WithUserID(c.Request.Context(), userID))
		}
		c.Next()
	}
}

func currentUser(c *gin.Context) string {
	value, _ := c.Get("user_id")
	userID, _ := value.(string)
	return userID
}

func readBodyPreview(c *gin.Context) string {
	if c.Request.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 256))
	if err != nil {
		return ""
	}
	return string(raw)
}

// adminAccountNames returns the comma-separated admin account names from
// ADMIN_ACCOUNT_NAMES. Used by moderator routes (suspend/restore worlds).
func adminAccountNames() []string {
	raw := os.Getenv("ADMIN_ACCOUNT_NAMES")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var names []string
	for _, part := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(part); name != "" {
			names = append(names, strings.ToLower(name))
		}
	}
	return names
}

// requireAdmin returns a middleware that rejects non-admin accounts.
func requireAdmin(db *sql.DB, admins []string) gin.HandlerFunc {
	lookup := make(map[string]struct{}, len(admins))
	for _, name := range admins {
		lookup[name] = struct{}{}
	}
	return func(c *gin.Context) {
		userID := currentUser(c)
		if userID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": "authentication_required", "message": "请先登录"})
			c.Abort()
			return
		}
		var accountName string
		err := db.QueryRowContext(c.Request.Context(), `SELECT account_name_normalized FROM users WHERE id=UUID_TO_BIN(?,1) AND status='active'`, userID).Scan(&accountName)
		if err != nil || len(lookup) == 0 {
			c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "当前账户没有管理权限"})
			c.Abort()
			return
		}
		if _, ok := lookup[accountName]; !ok {
			c.JSON(http.StatusForbidden, gin.H{"code": "forbidden", "message": "当前账户没有管理权限"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func authenticationKeys() ([]byte, []byte) {
	session := []byte(os.Getenv("AUTH_SESSION_DIGEST_KEY"))
	recovery := []byte(os.Getenv("AUTH_RECOVERY_PEPPER"))
	if len(session) >= 32 && len(recovery) >= 32 {
		return session, recovery
	}
	if os.Getenv("APP_ENV") != "development" {
		panic("AUTH_SESSION_DIGEST_KEY and AUTH_RECOVERY_PEPPER must each contain at least 32 bytes")
	}
	devSession := sha256.Sum256([]byte("novelstudio-development-session-digest-key"))
	devRecovery := sha256.Sum256([]byte("novelstudio-development-recovery-pepper"))
	return devSession[:], devRecovery[:]
}

func streamJob(c *gin.Context, store *turnjobs.Store) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	lastEvent := c.GetHeader("Last-Event-ID")
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(125 * time.Second)
	defer timeout.Stop()
	for {
		job, err := store.Get(c.Request.Context(), currentUser(c), c.Param("id"))
		if err != nil {
			writeSSE(c, "", "error", gin.H{"code": "job_unavailable"})
			c.Writer.Flush()
			return
		}
		eventID := statusEventID(job)
		if eventID != lastEvent {
			writeSSE(c, eventID, "job.status", gin.H{
				"jobId":     job.ID,
				"kind":      "turn",
				"status":    job.Status,
				"attemptNo": job.AttemptNo,
				"canCancel": canCancelTurnJob(job.Status),
				"errorCode": job.ErrorCode,
			})
			lastEvent = eventID
			if job.Status == turnjobs.StatusCompleted && job.CommittedTurn != nil {
				writeSSE(c, eventID+":turn", "turn.committed", job.CommittedTurn)
				c.Writer.Flush()
				return
			}
			if job.Status == turnjobs.StatusFailed || job.Status == turnjobs.StatusCancelled {
				c.Writer.Flush()
				return
			}
			c.Writer.Flush()
		}
		select {
		case <-c.Request.Context().Done():
			return
		case <-timeout.C:
			return
		case <-ticker.C:
		}
	}
}

func writeSSE(c *gin.Context, id, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		payload = []byte(`{"code":"event_encoding_failed"}`)
	}
	if id != "" {
		_, _ = fmt.Fprintf(c.Writer, "id:%s\n", id)
	}
	_, _ = fmt.Fprintf(c.Writer, "event:%s\ndata:%s\n\n", event, payload)
}

func statusEventID(job turnjobs.Job) string {
	return job.ID + ":" + string(job.Status) + ":" + strconv.Itoa(job.AttemptNo)
}

func canCancelTurnJob(status turnjobs.Status) bool {
	switch status {
	case turnjobs.StatusQueued, turnjobs.StatusPlanning, turnjobs.StatusDrafting, turnjobs.StatusValidating:
		return true
	default:
		return false
	}
}

func writeJobError(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "这一幕没有完成，微光已退回"
	switch {
	case errors.Is(err, turnjobs.ErrNotFound), errors.Is(err, turnjobs.ErrForbidden):
		status, code, message = http.StatusNotFound, "not_found", "旅程或任务不存在"
	case errors.Is(err, turnjobs.ErrInsufficientCredits):
		status, code, message = http.StatusPaymentRequired, "insufficient_credits", "当前微光不足"
	case errors.Is(err, turnjobs.ErrActiveJobExists), errors.Is(err, turnjobs.ErrConcurrentState), errors.Is(err, turnjobs.ErrIdempotencyConflict):
		status, code, message = http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, turnjobs.ErrCannotCancel), errors.Is(err, turnjobs.ErrCannotRetry):
		status, code, message = http.StatusConflict, "invalid_job_state", err.Error()
	default:
		var validation storyengine.ValidationError
		if errors.As(err, &validation) {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "proposal_rejected", "issues": validation.Issues})
			return
		}
	}
	c.JSON(status, gin.H{"code": code, "message": message})
}

func requestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
