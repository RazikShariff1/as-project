package main

import (
	"strconv"

	"gofr.dev/pkg/gofr"

	"github.com/zopping/account-svc/common/authctx"
	"github.com/zopping/account-svc/common/password"
	authHTTP "github.com/zopping/account-svc/http/auth"
	endpointHTTP "github.com/zopping/account-svc/http/endpoint"
	roleHTTP "github.com/zopping/account-svc/http/role"
	userHTTP "github.com/zopping/account-svc/http/user"
	"github.com/zopping/account-svc/migrations"
	authService "github.com/zopping/account-svc/services/auth"
	endpointService "github.com/zopping/account-svc/services/endpoint"
	roleService "github.com/zopping/account-svc/services/role"
	userService "github.com/zopping/account-svc/services/user"
	endpointStore "github.com/zopping/account-svc/stores/endpoint"
	roleStore "github.com/zopping/account-svc/stores/role"
	sessionStore "github.com/zopping/account-svc/stores/session"
	userStore "github.com/zopping/account-svc/stores/user"
)

const minutesPerSecond = 60

func main() {
	app := gofr.New()

	app.Migrate(migrations.All())
	app.UseMiddleware(authctx.Middleware())

	minutes, err := strconv.Atoi(app.Config.GetOrDefault("SESSION_TTL_MINUTES", "720"))
	if err != nil || minutes < 1 {
		app.Logger().Fatal("SESSION_TTL_MINUTES must be a positive number of minutes")
	}

	// Stores -> services -> handlers.
	users, roles, endpoints, sessions := userStore.New(), roleStore.New(), endpointStore.New(), sessionStore.New()
	hasher := password.New(password.DefaultCost)

	auth, err := authService.New(users, roles, sessions, hasher, minutes*minutesPerSecond)
	if err != nil {
		app.Logger().Fatalf("cannot start auth service: %v", err)
	}

	userHandler := userHTTP.New(userService.New(users, sessions, hasher))
	roleHandler := roleHTTP.New(roleService.New(roles))
	endpointHandler := endpointHTTP.New(endpointService.New(endpoints))
	authHandler := authHTTP.New(auth)

	// The first admin is created only while there are no users at all.
	adminEmail, adminPassword := app.Config.Get("ADMIN_EMAIL"), app.Config.Get("ADMIN_PASSWORD")

	app.OnStart(func(ctx *gofr.Context) error {
		if adminEmail == "" && adminPassword == "" {
			return nil
		}

		created, err := auth.Bootstrap(ctx, adminEmail, adminPassword)
		if err != nil {
			return err
		}

		if created {
			ctx.Infof("created the first admin account %s", adminEmail)
		}

		return nil
	})

	// This is the surface the gateway and clients call. /auth/check is the gateway's per-request permission check;
	// login is a POST, so GoFr answers it 201.
	app.POST("/auth/login", authHandler.Login)
	app.GET("/auth/me", authHandler.Me)
	app.GET("/auth/check", authHandler.Check)
	app.DELETE("/auth/session", authHandler.Logout)

	app.GET("/user", userHandler.Index)
	app.POST("/user", userHandler.Create)
	app.GET("/user/{id}", userHandler.Read)
	app.PUT("/user/{id}", userHandler.Update)
	app.DELETE("/user/{id}", userHandler.Delete)
	app.PUT("/user/{id}/password", userHandler.ChangePassword)
	app.PUT("/user/{id}/password/reset", userHandler.ResetPassword)
	app.DELETE("/user/{id}/session", userHandler.RevokeSessions)

	app.GET("/role", roleHandler.Index)
	app.POST("/role", roleHandler.Create)
	app.GET("/role/{id}", roleHandler.Read)
	app.PUT("/role/{id}", roleHandler.Update)
	app.DELETE("/role/{id}", roleHandler.Delete)

	app.GET("/endpoint", endpointHandler.Index)
	app.POST("/endpoint", endpointHandler.Create)
	app.GET("/endpoint/{id}", endpointHandler.Read)
	app.PUT("/endpoint/{id}", endpointHandler.Update)
	app.DELETE("/endpoint/{id}", endpointHandler.Delete)

	app.Run()
}
