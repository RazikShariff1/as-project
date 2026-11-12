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

	var out sender.Sender = sender.Log{Logger: app.Logger()}

	if host := app.Config.Get("SMTP_HOST"); host != "" {
		from, err := mail.ParseAddress(app.Config.Get("SMTP_FROM"))
		if err != nil {
			app.Logger().Fatal("SMTP_FROM must be an email address when SMTP_HOST is set")
		}

		from.Name = app.Config.Get("SMTP_FROM_NAME")
		out = &sender.SMTP{
			Host: host, Port: app.Config.GetOrDefault("SMTP_PORT", "587"),
			Username: app.Config.Get("SMTP_USERNAME"), Password: app.Config.Get("SMTP_PASSWORD"), From: *from,
		}
	} else {
		app.Logger().Warn("SMTP_HOST is not set: emails are logged, not delivered")
	}

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
