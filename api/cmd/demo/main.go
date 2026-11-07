// Command demo seeds a SQLite database with a week of synthetic history and the
// demo collectors, so people can try Signal Atlas without any devices.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"signalatlas/api/internal/collector"
	"signalatlas/api/internal/domain"
	"signalatlas/api/internal/repository"
)

func main() {
	out := flag.String("sqlite", "", "target SQLite file")
	days := flag.Int("days", 7, "days of history to generate")
	step := flag.Duration("step", 5*time.Minute, "sampling step")
	flag.Parse()
	if *out == "" {
		log.Fatal("用法：demo -sqlite /data/history.db [-days 7]")
	}
	ctx := context.Background()
	store, err := repository.NewSQLite(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	profiles := collector.DemoProfiles()
	if err = store.Write(ctx, "config", domain.Config{Profiles: profiles, Interval: 30}); err != nil {
		log.Fatal(err)
	}
	start := time.Now().Add(-time.Duration(*days) * 24 * time.Hour)
	count := 0
	for at := start; at.Before(time.Now()); at = at.Add(*step) {
		if err = store.Append(ctx, collector.DemoObservation(profiles, at)); err != nil {
			log.Fatal(err)
		}
		count++
	}
	fmt.Printf("已生成演示数据：%d 个采样点，%d 天，写入 %s\n", count, *days, *out)
	_ = os.Getenv
}
