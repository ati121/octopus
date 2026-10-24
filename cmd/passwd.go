package cmd

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/spf13/cobra"
)

// passwdCmd 提供忘记管理台密码时的命令行重置入口，只改密码不改用户名。
// 运行中的服务在内存里缓存了用户信息，重置后需重启服务才生效。
var passwdCmd = &cobra.Command{
	Use:   "passwd <password>",
	Short: "Set user password",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if args[0] == "" {
			return fmt.Errorf("password must not be empty")
		}
		if err := conf.Load(cfgFile); err != nil {
			return err
		}
		if err := db.InitDB(conf.AppConfig.Database.Type, conf.AppConfig.Database.Path, conf.IsDebug()); err != nil {
			return fmt.Errorf("database init error: %w", err)
		}
		defer db.Close()

		var user model.User
		if err := db.GetDB().First(&user).Error; err != nil {
			return fmt.Errorf("failed to load user: %w", err)
		}
		user.Password = args[0]
		if err := user.HashPassword(); err != nil {
			return err
		}
		if err := db.GetDB().Model(&user).Update("password", user.Password).Error; err != nil {
			return fmt.Errorf("failed to update password: %w", err)
		}

		cmd.Println("Password updated. Restart the running server to apply the change.")
		return nil
	},
}

func init() {
	passwdCmd.Flags().StringVar(&cfgFile, "config", "", "config file (default is ./data/config.json)")
	rootCmd.AddCommand(passwdCmd)
}
