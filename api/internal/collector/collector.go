package collector

import (
	"context"
	"errors"
	"net"
	"signalatlas/api/internal/domain"
	"strings"
	"time"
)

func Collect(ctx context.Context, p domain.Profile, password, knownHosts string) domain.Result {
	start := time.Now()
	result := domain.Result{Profile: p, Status: "ok", CollectedAt: start, Data: map[string]any{"devices": []domain.Device{}, "stations": []domain.Station{}, "radios": []domain.Radio{}}}
	if p.Adapter == "demo" || strings.HasPrefix(p.ID, "demo-") {
		data, err := demoCollect(p)
		if err != nil {
			result.Status, result.Error = "error", err.Error()
			return result
		}
		for k, v := range data {
			result.Data[k] = v
		}
		result.Duration = time.Since(start).Seconds()
		return result
	}
	if p.Adapter != "tcp" && password == "" {
		result.Status = "needs_auth"
		result.Error = "待输入管理凭据"
		return result
	}
	var data map[string]any
	var err error
	switch p.Adapter {
	case "openwrt":
		data, err = collectRouter(ctx, p, password, knownHosts)
	case "asus":
		data, err = collectASUS(ctx, p, password, knownHosts)
	case "zte":
		data, err = collectZTE(ctx, p, password)
	case "esxi":
		data, err = collectESXi(ctx, p, password)
	case "tcp":
		t := time.Now()
		c, e := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(p.Host, "80"))
		if e != nil {
			err = errors.New("管理端口80未响应")
		} else {
			c.Close()
			data = map[string]any{"model": "管理口连通性", "latency_ms": float64(time.Since(t).Microseconds()) / 1000, "note": "仅检查端口连通性，不代表已读取交换机内部统计", "devices": []domain.Device{}, "stations": []domain.Station{}, "radios": []domain.Radio{}}
		}
	default:
		err = errors.New("不支持的设备适配器")
	}
	result.Duration = time.Since(start).Seconds()
	result.CollectedAt = time.Now()
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
	} else {
		result.Data = data
	}
	return result
}
