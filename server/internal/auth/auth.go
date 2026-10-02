// Package auth 提供短信验证码的生成/存储/校验。
// 测试模式下不真正发送短信，验证码固定为 111111；正式模式接短信通道时替换 sendReal 即可。
package auth

import (
	"fmt"
	"math/rand"
	"sync"
	"time"
)

var TestCode = "111111"

type otpEntry struct {
	code    string
	expires time.Time
}

type OTPStore struct {
	mu   sync.Mutex
	codes map[string]otpEntry
	ttl  time.Duration
}

func NewOTPStore(ttl time.Duration) *OTPStore {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &OTPStore{codes: map[string]otpEntry{}, ttl: ttl}
}

func randomCode() string {
	return fmt.Sprintf("%06d", rand.Intn(1000000))
}

// Send 生成验证码并发送（测试模式下固定 111111 直接返回用于登录）。
func (o *OTPStore) Send(phone string, testMode bool) (sentCode string) {
	code := TestCode
	if !testMode {
		code = randomCode()
		// 正式短信通道接入点：真实发送到这里（如腾讯云/Aliyun）。
		_ = sendReal(phone, code)
	}
	o.mu.Lock()
	o.codes[phone] = otpEntry{code: code, expires: time.Now().Add(o.ttl)}
	o.mu.Unlock()
	return code
}

// Verify 校验验证码，一次性（成功后删除）。测试/真实同规则。
func (o *OTPStore) Verify(phone, code string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	e, ok := o.codes[phone]
	if !ok || time.Now().After(e.expires) || e.code != code {
		return false
	}
	delete(o.codes, phone)
	return true
}

// sendReal 占位：正式短信通道的实现点。
func sendReal(phone, code string) error {
	// TODO: 接入短信服务商（腾讯云/Aliyun 等）后在此真实发送。
	return nil
}