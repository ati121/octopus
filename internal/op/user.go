package op

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

var userCache model.User
var userCacheMu sync.RWMutex

func UserInit() error {
	userCacheMu.Lock()
	defer userCacheMu.Unlock()
	var user model.User
	if err := db.GetDB().First(&user).Error; err == nil {
		userCache = user
		return nil
	}
	user.Username = "admin"
	user.Password = "admin"
	if err := user.HashPassword(); err != nil {
		return err
	}
	if err := db.GetDB().Create(&user).Error; err != nil {
		return err
	}
	userCache = user
	log.Infof("initial user: admin,password: admin")
	return nil
}

func UserChangePassword(oldPassword, newPassword string) error {
	userCacheMu.Lock()
	defer userCacheMu.Unlock()
	if err := userCache.ComparePassword(oldPassword); err != nil {
		return apperror.Wrap(apperror.CodeAuthPasswordIncorrect, "incorrect old password", err).WithStatus(http.StatusBadRequest)
	}

	updated := userCache
	updated.Password = newPassword
	if err := updated.HashPassword(); err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	if err := db.GetDB().Model(&updated).Update("password", updated.Password).Error; err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	userCache = updated

	return nil
}

func UserChangeUsername(newUsername string) error {
	userCacheMu.Lock()
	defer userCacheMu.Unlock()
	if userCache.Username == newUsername {
		return fmt.Errorf("new username is the same as the old username")
	}
	updated := userCache
	updated.Username = newUsername
	if err := db.GetDB().Model(&updated).Update("username", updated.Username).Error; err != nil {
		return fmt.Errorf("failed to update username: %w", err)
	}
	userCache = updated
	return nil
}

func UserVerify(username, password string) error {
	_, err := UserAuthenticate(username, password)
	return err
}

// 返回已验证的密码快照，使并发改密之前开始的登录不能签发新密码的会话。
func UserAuthenticate(username, password string) (model.User, error) {
	user := UserGet()
	if username != user.Username {
		return model.User{}, fmt.Errorf("incorrect username")
	}
	if err := user.ComparePassword(password); err != nil {
		return model.User{}, fmt.Errorf("incorrect password")
	}
	return user, nil
}

func UserGet() model.User {
	userCacheMu.RLock()
	defer userCacheMu.RUnlock()
	return userCache
}
