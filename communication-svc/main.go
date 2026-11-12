package main

import (
	"net/mail"
	"strconv"

	"gofr.dev/pkg/gofr"

	emailHTTP "github.com/zopping/communication-svc/http/email"
	templateHTTP "github.com/zopping/communication-svc/http/template"
	"github.com/zopping/communication-svc/migrations"
	"github.com/zopping/communication-svc/sender"
	emailService "github.com/zopping/communication-svc/services/email"
	templateService "github.com/zopping/communication-svc/services/template"
	emailStore "github.com/zopping/communication-svc/stores/email"
	templateStore "github.com/zopping/communication-svc/stores/template"
)

func main() {
	app := gofr.New()

	app.Migrate(migrations.All())

	maxAttempts, err := strconv.Atoi(app.Config.GetOrDefault("EMAIL_MAX_ATTEMPTS", "5"))
	if err != nil || maxAttempts < 1 {
		app.Logger().Fatal("EMAIL_MAX_ATTEMPTS must be a positive number")
	}

	out := newSender(app)

	// Stores -> services -> handlers.
	templates := templateStore.New()
	emails := emailService.New(emailStore.New(), templates, out, maxAttempts)
	emailHandler := emailHTTP.New(emails)
	templateHandler := templateHTTP.New(templateService.New(templates))

	// This service trusts its callers: it is internal and not on the gateway. Anyone who can reach it can send mail.
	app.POST("/email", emailHandler.Create)
	app.GET("/email", emailHandler.Index)
	app.GET("/email/{id}", emailHandler.Read)

	app.GET("/communications", templateHandler.Index)
	app.POST("/communications", templateHandler.Create)
	app.GET("/communications/{id}", templateHandler.Read)
	app.PUT("/communications/{id}", templateHandler.Update)
	app.DELETE("/communications/{id}", templateHandler.Delete)

	// The delivery worker: every five seconds, send what is due. It also retries failures and rescues emails a crashed
	// worker left half-sent.
	app.AddCronJob("*/5 * * * * *", "deliver-emails", func(ctx *gofr.Context) {
		sent, failed, err := emails.Deliver(ctx)
		if err != nil {
			ctx.Errorf("delivering emails: %v", err)
		}

		if sent+failed > 0 {
			ctx.Infof("emails delivered: %d sent, %d failed (will retry or give up)", sent, failed)
		}
	})

	app.Run()
}

// newSender picks how mail leaves: the Gmail API when GMAIL_REFRESH_TOKEN is set (HTTPS, so it works on Render's free plan,
// which blocks SMTP ports), else SMTP when SMTP_HOST is set, else just the log.
func newSender(app *gofr.App) sender.Sender {
	cfg := app.Config

	if cfg.Get("GMAIL_REFRESH_TOKEN") != "" {
		from, err := mail.ParseAddress(cfg.Get("GMAIL_FROM"))
		if err != nil || cfg.Get("GMAIL_CLIENT_ID") == "" || cfg.Get("GMAIL_CLIENT_SECRET") == "" {
			app.Logger().Fatal("with GMAIL_REFRESH_TOKEN, set GMAIL_CLIENT_ID, GMAIL_CLIENT_SECRET and GMAIL_FROM (the Gmail address)")
		}

		from.Name = cfg.Get("MAIL_FROM_NAME")

		return &sender.Gmail{
			ClientID: cfg.Get("GMAIL_CLIENT_ID"), ClientSecret: cfg.Get("GMAIL_CLIENT_SECRET"),
			RefreshToken: cfg.Get("GMAIL_REFRESH_TOKEN"), From: *from,
		}
	}

	if host := cfg.Get("SMTP_HOST"); host != "" {
		from, err := mail.ParseAddress(cfg.Get("SMTP_FROM"))
		if err != nil {
			app.Logger().Fatal("SMTP_FROM must be an email address when SMTP_HOST is set")
		}

		from.Name = cfg.Get("MAIL_FROM_NAME")

		return &sender.SMTP{
			Host: host, Port: cfg.GetOrDefault("SMTP_PORT", "587"),
			Username: cfg.Get("SMTP_USERNAME"), Password: cfg.Get("SMTP_PASSWORD"), From: *from,
		}
	}

	app.Logger().Warn("neither GMAIL_REFRESH_TOKEN nor SMTP_HOST is set: emails are logged, not delivered")

	return sender.Log{Logger: app.Logger()}
}
