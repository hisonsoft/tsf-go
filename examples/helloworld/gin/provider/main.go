package main

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"
	"github.com/go-kratos/kratos/v2"
	"github.com/hisonsoft/tsf-go"
	tgin "github.com/hisonsoft/tsf-go/gin"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/transport/http"
)

type Reply struct {
	Message string `json:"message"`
}

func main() {
	router := gin.Default()
	router.Use(tgin.Middlewares(tsf.ServerMiddleware()))

	router.GET("/helloworld/:name", func(ctx *gin.Context) {
		name := ctx.Param("name")
		if name != "error" {
			ctx.JSON(200, Reply{Message: fmt.Sprintf("Hello %v!", name)})
		} else {
			tgin.Error(ctx, errors.Unauthorized("auth_error", "no authentication"))
		}
	})

	httpSrv := http.NewServer(http.Address(":8000"))
	httpSrv.HandlePrefix("/", router)

	opts := []kratos.Option{kratos.Name("provider-http"), kratos.Server(httpSrv)}
	opts = append(opts, tsf.AppOptions()...)
	app := kratos.New(opts...)

	if err := app.Run(); err != nil {
		log.Println(err)
	}
}
