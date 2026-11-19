package auth

import (
	"bytes"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/golang-jwt/jwt/v5"
)

func setupAuthTest(t *testing.T) {
	t.Helper()
	if err := db.InitDB("sqlite", filepath.Join(t.TempDir(), "auth.db"), false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := op.InitCache(); err != nil {
		t.Fatal(err)
	}
	if err := op.UserInit(); err != nil {
		t.Fatal(err)
	}
	jwtSecretOnce, jwtSecretKey = sync.Once{}, nil
}

func testLoginToken(t *testing.T) string {
	t.Helper()
	token, _, err := GenerateJWTToken(op.UserGet(), -1)
	if err != nil || !VerifyJWTToken(token) {
		t.Fatalf("签发会话失败: %v", err)
	}
	return token
}

func TestPasswordChangeRevokesOldSessions(t *testing.T) {
	setupAuthTest(t)
	verified, err := op.UserAuthenticate("admin", "admin")
	if err != nil {
		t.Fatal(err)
	}
	oldToken := testLoginToken(t)
	if err := op.UserChangePassword("admin", "new-test-password"); err != nil {
		t.Fatal(err)
	}
	if VerifyJWTToken(oldToken) {
		t.Fatal("修改密码后旧会话仍有效")
	}
	if _, err := op.UserAuthenticate("admin", "new-test-password"); err != nil {
		t.Fatal(err)
	}
	testLoginToken(t)
	// 改密前开始、改密后才签发的登录只能得到已经失效的旧会话。
	lateToken, _, err := GenerateJWTToken(verified, -1)
	if err != nil {
		t.Fatal(err)
	}
	if VerifyJWTToken(lateToken) {
		t.Fatal("并发登录用旧密码签发了新密码下有效的会话")
	}
}

func TestSessionSurvivesReloadWithoutPasswordChange(t *testing.T) {
	setupAuthTest(t)
	token := testLoginToken(t)
	secret := append([]byte(nil), jwtSecretKey...)
	jwtSecretOnce, jwtSecretKey = sync.Once{}, nil
	if err := op.InitCache(); err != nil {
		t.Fatal(err)
	}
	if err := op.UserInit(); err != nil {
		t.Fatal(err)
	}
	if !VerifyJWTToken(token) || !bytes.Equal(secret, jwtSecretKey) {
		t.Fatal("未改密时重载破坏了已持久化的会话")
	}
}

func TestPasswordResetRevokesSessionsAfterReload(t *testing.T) {
	setupAuthTest(t)
	token := testLoginToken(t)
	// 与 passwd 命令相同：直接更新数据库密码，再由服务重启加载。
	user := op.UserGet()
	user.Password = "cli-reset-test-password"
	if err := user.HashPassword(); err != nil {
		t.Fatal(err)
	}
	if err := db.GetDB().Model(&user).Update("password", user.Password).Error; err != nil {
		t.Fatal(err)
	}
	jwtSecretOnce, jwtSecretKey = sync.Once{}, nil
	if err := op.UserInit(); err != nil {
		t.Fatal(err)
	}
	if VerifyJWTToken(token) {
		t.Fatal("命令行重置密码并重载后旧会话仍有效")
	}
	testLoginToken(t)
}

func TestFailedPasswordChangeKeepsLoginAndSession(t *testing.T) {
	setupAuthTest(t)
	token := testLoginToken(t)
	if err := op.UserChangePassword("admin", strings.Repeat("a", 73)); err == nil {
		t.Fatal("bcrypt 应拒绝超过 72 字节的密码")
	}
	if _, err := op.UserAuthenticate("admin", "admin"); err != nil || !VerifyJWTToken(token) {
		t.Fatal("哈希失败污染了原密码或会话")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := op.UserChangePassword("admin", "new-test-password"); err == nil {
		t.Fatal("关闭的数据库不能成功改密")
	}
	if _, err := op.UserAuthenticate("admin", "admin"); err != nil || !VerifyJWTToken(token) {
		t.Fatal("持久化失败污染了原密码或会话")
	}
}

func TestVerifyJWTRequiresExpectedClaimsAndAlgorithm(t *testing.T) {
	setupAuthTest(t)
	for _, name := range []string{"missing-expiry", "expired", "wrong-issuer", "wrong-algorithm"} {
		t.Run(name, func(t *testing.T) {
			claims := jwt.RegisteredClaims{Issuer: conf.APP_NAME, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}
			method := jwt.SigningMethodHS256
			switch name {
			case "missing-expiry":
				claims.ExpiresAt = nil
			case "expired":
				claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
			case "wrong-issuer":
				claims.Issuer = "other-app"
			case "wrong-algorithm":
				method = jwt.SigningMethodHS384
			}
			token, err := jwt.NewWithClaims(method, claims).SignedString(getJWTSecret(op.UserGet().Password))
			if err != nil {
				t.Fatal(err)
			}
			if VerifyJWTToken(token) {
				t.Fatal("接受了无效会话")
			}
		})
	}
}
