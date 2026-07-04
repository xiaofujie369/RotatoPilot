package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv, Port, PublicURL, AdminUsername, AdminPassword, JWTSecret, EncryptionKey, DBPath string
	AutoRotateGlobal, RequireConfirmation, TelegramEnabled                                  bool
	CheckInterval, RotationCooldown, ChangeIPWait, ChangeIPPollTimeout                      time.Duration
	FailureThreshold, SuccessRecovery, MaxRotationsPerDay                                   int
	TelegramBotToken, TelegramChatID, LogLevel                                              string
}

func Load() (Config, error) {
	c := Config{
		AppEnv: env("APP_ENV", "production"), Port: env("APP_PORT", "8080"), PublicURL: strings.TrimRight(env("PUBLIC_URL", "http://127.0.0.1:8080"), "/"),
		AdminUsername: env("ADMIN_USERNAME", "admin"), AdminPassword: env("ADMIN_PASSWORD", "change_me"),
		JWTSecret: env("JWT_SECRET", "change_me"), EncryptionKey: env("APP_ENCRYPTION_KEY", "change_me_32_bytes_random_string"),
		DBPath: env("DB_PATH", "./data/app.db"), AutoRotateGlobal: boolEnv("AUTO_ROTATE_GLOBAL_ENABLED", false),
		RequireConfirmation: boolEnv("REQUIRE_CONFIRMATION_CHECK", true), TelegramEnabled: boolEnv("TELEGRAM_ENABLED", false),
		CheckInterval: durationEnv("CHECK_INTERVAL_SECONDS", 300), RotationCooldown: durationMinutesEnv("ROTATION_COOLDOWN_MINUTES", 30),
		ChangeIPWait: durationEnv("CHANGE_IP_WAIT_SECONDS", 180), ChangeIPPollTimeout: durationEnv("CHANGE_IP_POLL_TIMEOUT_SECONDS", 600),
		FailureThreshold: intEnv("FAILURE_THRESHOLD", 3), SuccessRecovery: intEnv("SUCCESS_RECOVERY_THRESHOLD", 2),
		MaxRotationsPerDay: intEnv("MAX_ROTATIONS_PER_DAY", 10), TelegramBotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID: os.Getenv("TELEGRAM_CHAT_ID"), LogLevel: env("LOG_LEVEL", "info"),
	}
	if c.JWTSecret == "" || c.EncryptionKey == "" {
		return c, fmt.Errorf("JWT_SECRET and APP_ENCRYPTION_KEY are required")
	}
	if c.AppEnv == "production" && (strings.HasPrefix(c.JWTSecret, "change_me") || strings.HasPrefix(c.EncryptionKey, "change_me")) {
		return c, fmt.Errorf("production requires random JWT_SECRET and APP_ENCRYPTION_KEY values")
	}
	return c, nil
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func boolEnv(k string, d bool) bool {
	v, e := strconv.ParseBool(env(k, strconv.FormatBool(d)))
	if e != nil {
		return d
	}
	return v
}
func intEnv(k string, d int) int {
	v, e := strconv.Atoi(env(k, strconv.Itoa(d)))
	if e != nil {
		return d
	}
	return v
}
func durationEnv(k string, d int) time.Duration { return time.Duration(intEnv(k, d)) * time.Second }
func durationMinutesEnv(k string, d int) time.Duration {
	return time.Duration(intEnv(k, d)) * time.Minute
}
