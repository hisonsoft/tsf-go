module github.com/hisonsoft/tsf-go/examples

go 1.15

require (
	github.com/gin-gonic/gin v1.7.3
	github.com/go-kratos/kratos/v2 v2.0.5
	github.com/go-redis/redis/v8 v8.11.2
	github.com/go-sql-driver/mysql v1.6.0
	github.com/luna-duclos/instrumentedsql v1.1.3
	github.com/hisonsoft/tsf-go v1.0.0
	github.com/hisonsoft/tsf-go/tracing/mysqlotel v1.0.0-rc1
	github.com/hisonsoft/tsf-go/tracing/redisotel v1.0.0-rc1
	google.golang.org/genproto v0.0.0-20210811021853-ddbe55d93216
	google.golang.org/grpc v1.40.0
	google.golang.org/protobuf v1.27.1
)

replace github.com/hisonsoft/tsf-go => ../

replace github.com/hisonsoft/tsf-go/tracing/redisotel => ../tracing/redisotel

replace github.com/hisonsoft/tsf-go/tracing/mysqlotel => ../tracing/mysqlotel
