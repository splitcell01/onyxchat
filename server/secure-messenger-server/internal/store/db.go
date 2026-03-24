package store

import (
    "context"
    "database/sql"
    "log"
    "os"
    "time"

    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/ssm"
    _ "github.com/jackc/pgx/v5/stdlib"
)

func ResolveSecret(envKey, ssmPath string) string {
    if v := os.Getenv(envKey); v != "" {
        return v
    }
    cfg, err := config.LoadDefaultConfig(context.Background())
    if err != nil {
        log.Fatalf("failed to load AWS config: %v", err)
    }
    client := ssm.NewFromConfig(cfg)
    withDecryption := true
    out, err := client.GetParameter(context.Background(), &ssm.GetParameterInput{
        Name:           &ssmPath,
        WithDecryption: &withDecryption,
    })
    if err != nil {
        log.Fatalf("failed to fetch SSM param %s: %v", ssmPath, err)
    }
    return *out.Parameter.Value
}

func MustOpen() *sql.DB {
    dsn := ResolveSecret("SM_DB_DSN", "/onyxchat/prod/SM_DB_DSN")
    db, err := sql.Open("pgx", dsn)
    if err != nil {
        log.Fatalf("failed to open db: %v", err)
    }
    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(25)
    db.SetConnMaxLifetime(5 * time.Minute)
    if err := db.Ping(); err != nil {
        log.Fatalf("failed to ping db: %v", err)
    }
    return db
}
