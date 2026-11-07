// Command migrate copies an existing PostgreSQL deployment into a SQLite file
// for the single-binary layout. The source database is only read.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"signalatlas/api/internal/repository"
)

func main() {
	pg := flag.String("pg", os.Getenv("DATABASE_URL"), "PostgreSQL DSN (default: $DATABASE_URL)")
	out := flag.String("sqlite", "", "target SQLite file (must be absent or empty)")
	flag.Parse()
	if *pg == "" || *out == "" {
		log.Fatal("用法：migrate -pg <DSN> -sqlite <path>")
	}
	if info, err := os.Stat(*out); err == nil && info.Size() > 0 {
		log.Fatal("目标文件已存在且非空，请换一个路径")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	start := time.Now()
	counts, err := repository.MigrateFromPostgres(ctx, *pg, *out, func(line string) {
		fmt.Println(line)
	})
	if err != nil {
		log.Fatal(err)
	}
	total := int64(0)
	for name, n := range counts {
		fmt.Printf("%-22s %8d\n", name, n)
		total += n
	}
	fmt.Printf("完成：%d 行，用时 %s\n", total, time.Since(start).Round(time.Second))
}
