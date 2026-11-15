package telegram

import (
	"bufio"
	"context"
	"fmt"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/perso/perso-mind/internal/config"
	"golang.org/x/term"
	"os"
	"strings"
)

type terminalLogin struct {
	phone  string
	reader *bufio.Reader
}

func (t terminalLogin) Phone(context.Context) (string, error) { return t.phone, nil }
func (t terminalLogin) Code(context.Context, *tg.AuthSentCode) (string, error) {
	fmt.Fprint(os.Stderr, "Telegram sign-in code: ")
	text, err := t.reader.ReadString('\n')
	return strings.TrimSpace(text), err
}
func (t terminalLogin) Password(context.Context) (string, error) {
	fmt.Fprint(os.Stderr, "Telegram 2FA password: ")
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(secret), err
}
func (t terminalLogin) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return fmt.Errorf("create the Telegram account in the official app first")
}
func (t terminalLogin) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, fmt.Errorf("account sign-up is not supported")
}

// Login owns the same exclusive session lock as the gateway. Stop the gateway
// first; phone codes and passwords are never stored in configuration or logs.
func Login(ctx context.Context, cfg *config.Config) error {
	if cfg.TelegramAppID <= 0 || cfg.TelegramAppHash == "" || cfg.TelegramPhoneNumber == "" {
		return fmt.Errorf("Telegram app ID, app hash and phone number are required")
	}
	storage, err := NewEncryptedSessionStorage(cfg.TelegramSessionDir, "perso_session.dat", cfg.TelegramSessionStorageKey)
	if err != nil {
		return err
	}
	if err = storage.LockSession(); err != nil {
		return err
	}
	defer storage.UnlockSession()
	client := telegram.NewClient(cfg.TelegramAppID, cfg.TelegramAppHash, telegram.Options{SessionStorage: storage})
	return client.Run(ctx, func(ctx context.Context) error {
		return client.Auth().IfNecessary(ctx, auth.NewFlow(terminalLogin{phone: cfg.TelegramPhoneNumber, reader: bufio.NewReader(os.Stdin)}, auth.SendCodeOptions{}))
	})
}
