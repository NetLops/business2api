package register

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"business2api/src/logger"
	"business2api/src/pool"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

var (
	RegisterDebug bool
	RegisterOnce  bool
	httpClient    *http.Client
	firstNames    = []string{"John", "Jane", "Michael", "Sarah", "David", "Emily", "Robert", "Lisa", "James", "Emma"}
	lastNames     = []string{"Smith", "Johnson", "Williams", "Brown", "Jones", "Garcia", "Miller", "Davis", "Wilson", "Taylor"}
	commonWords   = map[string]bool{
		"VERIFY": true, "GOOGLE": true, "UPDATE": true, "MOBILE": true, "DEVICE": true,
		"SUBMIT": true, "RESEND": true, "CANCEL": true, "DELETE": true, "REMOVE": true,
		"SEARCH": true, "VIDEOS": true, "IMAGES": true, "GMAIL": true, "EMAIL": true,
		"ACCOUNT": true, "CHROME": true,
	}
)

// SetHTTPClient 设置HTTP客户端
func SetHTTPClient(c *http.Client) {
	httpClient = c
}
func readResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	var reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
	}

	body := make([]byte, 0)
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			body = append(body, buf[:n]...)
		}
		if err != nil {
			break
		}
	}
	return body, nil
}

type TempEmailResponse struct {
	Email string `json:"email"`
	Data  struct {
		Email string `json:"email"`
	} `json:"data"`
}
type EmailListResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Emails []EmailContent `json:"emails"`
	} `json:"data"`
}
type EmailContent struct {
	Subject string `json:"subject"`
	Content string `json:"content"`
}
type BrowserRegisterResult struct {
	Success       bool
	Email         string
	FullName      string
	Authorization string
	Cookies       []pool.Cookie
	ConfigID      string
	CSESIDX       string
	Error         error
}

func generateRandomName() string {
	return firstNames[rand.Intn(len(firstNames))] + " " + lastNames[rand.Intn(len(lastNames))]
}

type TempMailProvider struct {
	Name        string
	GenerateURL string
	CheckURL    string
	Headers     map[string]string
}

// 支持的临时邮箱提供商列表
var tempMailProviders = []TempMailProvider{
	{
		Name:        "chatgpt.org.uk",
		GenerateURL: "https://mail.chatgpt.org.uk/api/generate-email",
		CheckURL:    "https://mail.chatgpt.org.uk/api/emails?email=%s",
		Headers: map[string]string{
			"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36",
			"Referer":    "https://mail.chatgpt.org.uk",
		},
	},
	// 备用邮箱服务可以在这里添加
}

func getTemporaryEmail() (string, error) {
	var lastErr error
	for _, provider := range tempMailProviders {
		for retry := 0; retry < 3; retry++ {
			email, err := getEmailFromProvider(provider)
			if err != nil {
				lastErr = err
				if retry < 2 {
					log.Printf("⚠️ 临时邮箱 %s 失败 (重试 %d/3): %v", provider.Name, retry+1, err)
					time.Sleep(time.Duration(retry+1) * time.Second)
					continue
				}
				log.Printf("⚠️ 临时邮箱 %s 失败，尝试下一个提供商", provider.Name)
				break
			}
			if !strings.Contains(email, "@") {
				lastErr = fmt.Errorf("邮箱格式无效: %s", email)
				continue
			}
			return email, nil
		}
	}
	return "", fmt.Errorf("所有临时邮箱服务均失败: %v", lastErr)
}
func getEmailFromProvider(provider TempMailProvider) (string, error) {
	req, _ := http.NewRequest("GET", provider.GenerateURL, nil)
	for k, v := range provider.Headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	if httpClient != nil {
		client = httpClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := readResponseBody(resp)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	var result TempEmailResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w, body: %s", err, string(body[:min(100, len(body))]))
	}

	email := result.Email
	if email == "" {
		email = result.Data.Email
	}
	if email == "" {
		return "", fmt.Errorf("返回的邮箱为空, 响应: %s", string(body[:min(100, len(body))]))
	}
	return email, nil
}
func getEmailCount(email string) int {
	// 重试3次
	for retry := 0; retry < 3; retry++ {
		req, _ := http.NewRequest("GET", fmt.Sprintf("https://mail.chatgpt.org.uk/api/emails?email=%s", email), nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
		req.Header.Set("Referer", "https://mail.chatgpt.org.uk")

		client := &http.Client{Timeout: 15 * time.Second}
		if httpClient != nil {
			client = httpClient
		}

		resp, err := client.Do(req)
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		defer resp.Body.Close()

		body, _ := readResponseBody(resp)
		var result EmailListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			continue
		}
		return len(result.Data.Emails)
	}
	return 0
}

type VerificationState struct {
	UsedCodes    map[string]bool // 已使用过的验证码
	LastEmailID  string          // 上次处理的邮件ID
	ResendCount  int             // 重发次数
	LastResendAt time.Time       // 上次重发时间
	mu           sync.Mutex
}

func NewVerificationState() *VerificationState {
	return &VerificationState{
		UsedCodes: make(map[string]bool),
	}
}

func (vs *VerificationState) MarkCodeUsed(code string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.UsedCodes[code] = true
}
func (vs *VerificationState) IsCodeUsed(code string) bool {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	return vs.UsedCodes[code]
}
func (vs *VerificationState) CanResend() bool {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	if vs.ResendCount >= 3 {
		return false
	}
	if time.Since(vs.LastResendAt) < 10*time.Second {
		return false
	}
	return true
}

// RecordResend 记录重发
func (vs *VerificationState) RecordResend() {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.ResendCount++
	vs.LastResendAt = time.Now()
}
func getVerificationEmailQuick(email string, retries int, intervalSec int) (*EmailContent, error) {
	return getVerificationEmailAfter(email, retries, intervalSec, 0)
}
func getVerificationEmailAfter(email string, retries int, intervalSec int, initialCount int) (*EmailContent, error) {
	return getVerificationEmailWithState(email, retries, intervalSec, initialCount, nil)
}
func getVerificationEmailWithState(email string, retries int, intervalSec int, initialCount int, state *VerificationState) (*EmailContent, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	if httpClient != nil {
		client = httpClient
	}
	for i := 0; i < retries; i++ {
		req, _ := http.NewRequest("GET", fmt.Sprintf("https://mail.chatgpt.org.uk/api/emails?email=%s", email), nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
		req.Header.Set("Referer", "https://mail.chatgpt.org.uk")

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[验证码] 获取邮件列表失败: %v", err)
			time.Sleep(time.Duration(intervalSec) * time.Second)
			continue
		}
		body, _ := readResponseBody(resp)
		resp.Body.Close()

		var result EmailListResponse
		if err := json.Unmarshal(body, &result); err != nil {
			time.Sleep(time.Duration(intervalSec) * time.Second)
			continue
		}
		if result.Success && len(result.Data.Emails) > initialCount {
			for idx := 0; idx < len(result.Data.Emails)-initialCount; idx++ {
				latestEmail := &result.Data.Emails[idx]
				code, err := extractVerificationCode(latestEmail.Content)
				if err != nil {
					continue
				}
				if state != nil && state.IsCodeUsed(code) {
					log.Printf("[验证码] 跳过已使用的验证码: %s", code)
					continue
				}
				return latestEmail, nil
			}
			log.Printf("[验证码] 所有新邮件的验证码均已使用，等待新邮件...")
		}
		time.Sleep(time.Duration(intervalSec) * time.Second)
	}
	return nil, fmt.Errorf("未收到新的验证码邮件")
}

// PageState 页面状态类型
type PageState int

const (
	PageStateUnknown PageState = iota
	PageStateEmailInput
	PageStateCodeInput
	PageStateNameInput
	PageStateLoggedIn
	PageStateError
)

func GetPageState(pageURL string) PageState {
	if pageURL == "" {
		return PageStateUnknown
	}
	if strings.Contains(pageURL, "accountverification.business.gemini.google") {
		return PageStateCodeInput
	}
	if strings.Contains(pageURL, "auth.business.gemini.google") {
		return PageStateEmailInput
	}
	if strings.Contains(pageURL, "business.gemini.google/admin/create") {
		return PageStateNameInput
	}
	if strings.Contains(pageURL, "business.gemini.google") &&
		!strings.Contains(pageURL, "auth.") &&
		!strings.Contains(pageURL, "accountverification.") &&
		!strings.Contains(pageURL, "/admin/create") {
		return PageStateLoggedIn
	}
	return PageStateUnknown
}

func GetPageStateString(state PageState) string {
	switch state {
	case PageStateEmailInput:
		return "邮箱输入"
	case PageStateCodeInput:
		return "验证码输入"
	case PageStateNameInput:
		return "名字输入"
	case PageStateLoggedIn:
		return "已登录"
	case PageStateError:
		return "错误页面"
	default:
		return "未知"
	}
}

// WaitForPageState 等待页面达到指定状态
func WaitForPageState(page *rod.Page, targetState PageState, timeout time.Duration) (PageState, error) {
	start := time.Now()
	for time.Since(start) < timeout {
		info, err := page.Info()
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		currentState := GetPageState(info.URL)
		if currentState == targetState {
			return currentState, nil
		}

		// 如果已经登录，直接返回
		if currentState == PageStateLoggedIn {
			return currentState, nil
		}

		time.Sleep(500 * time.Millisecond)
	}

	// 超时，返回当前状态
	info, _ := page.Info()
	if info != nil {
		return GetPageState(info.URL), fmt.Errorf("等待页面状态超时")
	}
	return PageStateUnknown, fmt.Errorf("等待页面状态超时")
}

// safeInputEmail 安全输入邮箱（带验证）
func safeInputEmail(page *rod.Page, email string) error {
	maxRetries := 3

	for retry := 0; retry < maxRetries; retry++ {
		// 清空输入框
		_, _ = page.Eval(`() => {
			const inputs = document.querySelectorAll('input[type="email"], input[type="text"], input:not([type])');
			for (const inp of inputs) {
				inp.value = '';
				inp.focus();
			}
		}`)
		time.Sleep(300 * time.Millisecond)

		// 使用JS直接设置值
		_, err := page.Eval(fmt.Sprintf(`() => {
			const inputs = document.querySelectorAll('input[type="email"], input[type="text"], input:not([type])');
			if (inputs.length > 0) {
				const input = inputs[0];
				input.value = %q;
				input.dispatchEvent(new Event('input', { bubbles: true }));
				input.dispatchEvent(new Event('change', { bubbles: true }));
				return true;
			}
			return false;
		}`, email))
		if err != nil {
			log.Printf("[邮箱输入] JS设置失败: %v, 尝试逐字符输入", err)
			// 回退到逐字符输入
			for _, char := range email {
				if err := page.Keyboard.Type(input.Key(char)); err != nil {
					return err
				}
				time.Sleep(30 * time.Millisecond)
			}
		}

		time.Sleep(500 * time.Millisecond)

		// 验证输入是否完整
		result, err := page.Eval(fmt.Sprintf(`() => {
			const inputs = document.querySelectorAll('input[type="email"], input[type="text"], input:not([type])');
			if (inputs.length > 0) {
				return inputs[0].value === %q;
			}
			return false;
		}`, email))

		if err == nil && result.Value.Bool() {
			log.Printf("[邮箱输入] 验证成功: %s", email)
			return nil
		}

		// 获取当前值用于调试
		currentVal, _ := page.Eval(`() => {
			const inputs = document.querySelectorAll('input[type="email"], input[type="text"], input:not([type])');
			if (inputs.length > 0) return inputs[0].value;
			return '';
		}`)
		if currentVal != nil {
			log.Printf("[邮箱输入] 验证失败 (重试 %d/%d), 当前值: %s, 期望值: %s",
				retry+1, maxRetries, currentVal.Value.Str(), email)
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("邮箱输入失败: 输入值不完整")
}

// safeInputCode 安全输入验证码（带验证）
func safeInputCode(page *rod.Page, code string) error {
	maxRetries := 3

	for retry := 0; retry < maxRetries; retry++ {
		// 清空输入框
		_, _ = page.Eval(`() => {
			const inputs = document.querySelectorAll('input');
			for (const inp of inputs) {
				inp.value = '';
				inp.focus();
			}
		}`)
		time.Sleep(300 * time.Millisecond)

		// 使用JS直接设置值
		_, _ = page.Eval(fmt.Sprintf(`() => {
			const inputs = document.querySelectorAll('input');
			if (inputs.length > 0) {
				const input = inputs[0];
				input.value = %q;
				input.dispatchEvent(new Event('input', { bubbles: true }));
				input.dispatchEvent(new Event('change', { bubbles: true }));
			}
		}`, code))

		time.Sleep(300 * time.Millisecond)

		// 验证输入是否完整
		result, err := page.Eval(fmt.Sprintf(`() => {
			const inputs = document.querySelectorAll('input');
			if (inputs.length > 0) {
				return inputs[0].value === %q;
			}
			return false;
		}`, code))

		if err == nil && result.Value.Bool() {
			log.Printf("[验证码输入] 验证成功: %s", code)
			return nil
		}

		log.Printf("[验证码输入] 验证失败 (重试 %d/%d)", retry+1, maxRetries)
		time.Sleep(300 * time.Millisecond)
	}

	return fmt.Errorf("验证码输入失败")
}

func extractVerificationCode(content string) (string, error) {
	re := regexp.MustCompile(`\b[A-Z0-9]{6}\b`)
	matches := re.FindAllString(content, -1)

	for _, code := range matches {
		if commonWords[code] {
			continue
		}
		if regexp.MustCompile(`[0-9]`).MatchString(code) {
			return code, nil
		}
	}

	for _, code := range matches {
		if !commonWords[code] {
			return code, nil
		}
	}

	re2 := regexp.MustCompile(`(?i)code\s*[:is]\s*([A-Z0-9]{6})`)
	if m := re2.FindStringSubmatch(content); len(m) > 1 {
		return m[1], nil
	}

	return "", fmt.Errorf("无法从邮件中提取验证码")
}
func safeType(page *rod.Page, text string, delay int) error {
	// 一次性设置输入框值（更稳定）
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// 先尝试使用JS直接设置值（更稳定）
	_, err := page.Eval(fmt.Sprintf(`() => {
		const inputs = document.querySelectorAll('input');
		if (inputs.length > 0) {
			const input = inputs[0];
			input.value = %q;
			input.dispatchEvent(new Event('input', { bubbles: true }));
			input.dispatchEvent(new Event('change', { bubbles: true }));
			return true;
		}
		return false;
	}`, text))
	if err == nil {
		time.Sleep(200 * time.Millisecond)
		return nil
	}

	// 回退到逐字符输入
	for _, char := range text {
		if err := page.Keyboard.Type(input.Key(char)); err != nil {
			return err
		}
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
	return nil
}

// debugScreenshot 调试截图
func debugScreenshot(page *rod.Page, threadID int, step string) {
	if !RegisterDebug {
		return
	}
	screenshotDir := filepath.Join(DataDir, "screenshots")
	os.MkdirAll(screenshotDir, 0755)

	filename := filepath.Join(screenshotDir, fmt.Sprintf("thread%d_%s_%d.png", threadID, step, time.Now().Unix()))
	data, err := page.Screenshot(true, nil)
	if err != nil {
		log.Printf("[注册 %d] 📸 截图失败: %v", threadID, err)
		return
	}
	if err := os.WriteFile(filename, data, 0644); err != nil {
		log.Printf("[注册 %d] 📸 保存截图失败: %v", threadID, err)
		return
	}
	log.Printf("[注册 %d] 📸 截图保存: %s", threadID, filename)
}

// handleAdditionalSteps 处理额外步骤（复选框等）
func handleAdditionalSteps(page *rod.Page, threadID int) bool {
	log.Printf("[注册 %d] 检查是否需要处理额外步骤...", threadID)

	hasAdditionalSteps := false

	// 首先检查是否有"出了点问题"错误页面，需要点击重试
	retryResult, _ := page.Eval(`() => {
		const pageText = document.body ? document.body.innerText : '';
		if (pageText.includes('出了点问题') || pageText.includes('Something went wrong') || 
			pageText.includes('error') || pageText.includes('错误')) {
			// 查找重试按钮
			const buttons = document.querySelectorAll('button');
			for (const btn of buttons) {
				const text = btn.textContent || '';
				if (text.includes('重试') || text.includes('Retry') || text.includes('再试')) {
					btn.click();
					return { clicked: true, action: 'retry' };
				}
			}
		}
		return { clicked: false };
	}`)

	if retryResult != nil && retryResult.Value.Get("clicked").Bool() {
		log.Printf("[注册 %d] 检测到错误页面，已点击重试按钮", threadID)
		time.Sleep(3 * time.Second)
		return true
	}

	// 检查是否需要同意条款（主要处理复选框）
	checkboxResult, _ := page.Eval(`() => {
		const checkboxes = document.querySelectorAll('input[type="checkbox"]');
		for (const checkbox of checkboxes) {
			if (!checkbox.checked) {
				checkbox.click();
				return { clicked: true };
			}
		}
		return { clicked: false };
	}`)

	if checkboxResult != nil && checkboxResult.Value.Get("clicked").Bool() {
		hasAdditionalSteps = true
		log.Printf("[注册 %d] 已勾选条款复选框", threadID)
		time.Sleep(1 * time.Second)
	}

	// 如果有额外步骤，尝试提交
	if hasAdditionalSteps {
		log.Printf("[注册 %d] 发现有额外步骤，尝试提交...", threadID)

		// 尝试提交额外信息
		for i := 0; i < 3; i++ {
			submitResult, _ := page.Eval(`() => {
				const submitButtons = [
					...document.querySelectorAll('button'),
					...document.querySelectorAll('input[type="submit"]')
				];
				
				for (const button of submitButtons) {
					if (!button.disabled && button.offsetParent !== null) {
						const text = button.textContent || '';
						if (text.includes('同意') || text.includes('Confirm') || 
							text.includes('继续') || text.includes('Next') || 
							text.includes('Submit') || text.includes('完成')) {
							button.click();
							return { clicked: true };
						}
					}
				}
				
				// 点击第一个可用的提交按钮
				for (const button of submitButtons) {
					if (!button.disabled && button.offsetParent !== null) {
						button.click();
						return { clicked: true };
					}
				}
				
				return { clicked: false };
			}`)

			if submitResult != nil && submitResult.Value.Get("clicked").Bool() {
				log.Printf("[注册 %d] 已提交额外信息", threadID)
				break
			}

			time.Sleep(1 * time.Second)
		}

		// 等待可能的跳转
		time.Sleep(3 * time.Second)
		return true
	}

	return false
}

// checkAndHandleAdminPage 检查并处理管理创建页面
func checkAndHandleAdminPage(page *rod.Page, threadID int) bool {
	currentURL := ""
	info, _ := page.Info()
	if info != nil {
		currentURL = info.URL
	}

	// 检查是否是管理创建页面
	if strings.Contains(currentURL, "/admin/create") {
		log.Printf("[注册 %d] 检测到管理创建页面，尝试完成设置...", threadID)

		// 尝试查找并点击继续按钮
		formCompleted, _ := page.Eval(`() => {
			let completed = false;
			
			// 查找并点击继续按钮
			const continueTexts = ['Continue', '继续', 'Next', 'Submit', 'Finish', '完成'];
			const allButtons = document.querySelectorAll('button');
			
			for (const button of allButtons) {
				if (button.offsetParent !== null && !button.disabled) {
					const text = (button.textContent || '').trim();
					if (continueTexts.some(t => text.includes(t))) {
						button.click();
						console.log('点击继续按钮:', text);
						completed = true;
						return completed;
					}
				}
			}
			
			// 如果没有找到特定按钮，尝试点击第一个可见按钮
			for (const button of allButtons) {
				if (button.offsetParent !== null && !button.disabled) {
					const text = button.textContent || '';
					if (text.trim() && !text.includes('Cancel') && !text.includes('取消')) {
						button.click();
						console.log('点击通用按钮:', text);
						completed = true;
						break;
					}
				}
			}
			
			return completed;
		}`)

		if formCompleted != nil && formCompleted.Value.Bool() {
			log.Printf("[注册 %d] 已处理管理表单，等待跳转...", threadID)
			time.Sleep(5 * time.Second)
			return true
		}
	}

	return false
}

func RunBrowserRegister(headless bool, proxy string, threadID int) (result *BrowserRegisterResult) {
	result = &BrowserRegisterResult{}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[注册 %d] ☠️ panic 恢复: %v", threadID, r)
			result.Error = fmt.Errorf("panic: %v", r)
		}
	}()

	// 获取临时邮箱
	email, err := getTemporaryEmail()
	if err != nil {
		result.Error = err
		return result
	}
	result.Email = email

	// 启动浏览器 - 优先使用系统浏览器
	l := launcher.New()

	// 检测系统浏览器（支持更多环境）
	systemBrowsers := []string{
		// Linux
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"/snap/bin/chromium",
		"/opt/google/chrome/chrome",
		// Docker/Alpine
		"/usr/bin/chromium-browser",
		"/usr/lib/chromium/chromium",
		// Windows
		"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
		// macOS
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
	}

	browserFound := false
	for _, path := range systemBrowsers {
		if _, err := os.Stat(path); err == nil {
			l = l.Bin(path)
			browserFound = true
			log.Printf("[注册 %d] 使用浏览器: %s", threadID, path)
			break
		}
	}

	if !browserFound {
		log.Printf("[注册 %d] ⚠️ 未找到系统浏览器，尝试使用 rod 自动下载", threadID)
	}

	// 设置启动参数（兼容更多环境）
	l = l.Headless(headless).
		Set("no-sandbox").
		Set("disable-setuid-sandbox").
		Set("disable-dev-shm-usage").
		Set("disable-gpu").
		Set("disable-software-rasterizer").
		Set("disable-blink-features", "AutomationControlled").
		Set("window-size", "1280,800").
		Set("lang", "zh-CN").
		Set("disable-extensions")

	if proxy != "" {
		l = l.Proxy(proxy)
	}

	url, err := l.Launch()
	if err != nil {
		result.Error = fmt.Errorf("启动浏览器失败: %w", err)
		return result
	}

	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		result.Error = fmt.Errorf("连接浏览器失败: %w", err)
		return result
	}
	defer browser.Close()

	browser = browser.Timeout(120 * time.Second)

	// 获取默认页面
	pages, err := browser.Pages()
	if err != nil {
		result.Error = fmt.Errorf("获取页面列表失败: %w", err)
		return result
	}

	var page *rod.Page
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
		if err != nil {
			result.Error = fmt.Errorf("创建新页面失败: %w", err)
			return result
		}
	}

	// 确保 page 不为 nil
	if page == nil {
		result.Error = fmt.Errorf("无法获取或创建浏览器页面")
		return result
	}

	// 设置视口和 User-Agent
	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:  1280,
		Height: 800,
	}); err != nil {
		log.Printf("[注册 %d] ⚠️ 设置视口失败: %v", threadID, err)
	}

	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	}); err != nil {
		log.Printf("[注册 %d] ⚠️ 设置 User-Agent 失败: %v", threadID, err)
	}

	// 监听请求以捕获 authorization
	var authorization string
	var configID, csesidx string

	go page.EachEvent(func(e *proto.NetworkRequestWillBeSent) {
		if auth, ok := e.Request.Headers["authorization"]; ok {
			if authStr := auth.String(); authStr != "" {
				authorization = authStr
			}
		}
		url := e.Request.URL
		if m := regexp.MustCompile(`/cid/([a-f0-9-]+)`).FindStringSubmatch(url); len(m) > 1 && configID == "" {
			configID = m[1]
		}
		if m := regexp.MustCompile(`[?&]csesidx=(\d+)`).FindStringSubmatch(url); len(m) > 1 && csesidx == "" {
			csesidx = m[1]
		}
	})()
	if err := page.Navigate("https://business.gemini.google"); err != nil {
		result.Error = fmt.Errorf("打开页面失败: %w", err)
		return result
	}
	page.WaitLoad()
	time.Sleep(500 * time.Millisecond)
	debugScreenshot(page, threadID, "01_page_loaded")
	if _, err := page.Timeout(20 * time.Second).Element("input"); err != nil {
		result.Error = fmt.Errorf("等待输入框超时: %w", err)
		return result
	}
	time.Sleep(300 * time.Millisecond)

	// 查找并输入邮箱
	log.Printf("[注册 %d] 准备输入邮箱: %s", threadID, email)

	// 查找邮箱输入框（支持多种选择器）
	inputResult, _ := page.Eval(`() => {
		// 尝试多种选择器
		const selectors = [
			'input[type="email"]',
			'input[name="identifier"]',
			'input[autocomplete="email"]',
			'input[autocomplete="username"]',
			'input:not([type="hidden"]):not([type="submit"])'
		];
		
		for (const sel of selectors) {
			const input = document.querySelector(sel);
			if (input && input.offsetParent !== null) {
				input.click();
				input.focus();
				return { found: true, selector: sel, tagName: input.tagName };
			}
		}
		
		// 兜底：查找所有可见input
		const inputs = document.querySelectorAll('input');
		for (const input of inputs) {
			if (input.offsetParent !== null && input.type !== 'hidden' && input.type !== 'submit') {
				input.click();
				input.focus();
				return { found: true, selector: 'fallback', tagName: input.tagName };
			}
		}
		
		return { found: false, inputCount: inputs.length };
	}`)

	if inputResult != nil {
	}

	time.Sleep(200 * time.Millisecond)

	// 使用改进的输入方法
	inputSuccess, _ := page.Eval(fmt.Sprintf(`() => {
		const selectors = [
			'input[type="email"]',
			'input[name="identifier"]',
			'input[autocomplete="email"]',
			'input[autocomplete="username"]',
			'input:not([type="hidden"]):not([type="submit"])'
		];
		
		for (const sel of selectors) {
			const input = document.querySelector(sel);
			if (input && input.offsetParent !== null) {
				input.focus();
				input.value = %q;
				input.dispatchEvent(new Event('input', { bubbles: true }));
				input.dispatchEvent(new Event('change', { bubbles: true }));
				return { success: true, value: input.value };
			}
		}
		return { success: false };
	}`, email))

	// 检查JS输入是否成功
	jsSuccess := false
	if inputSuccess != nil {
		jsSuccess = inputSuccess.Value.Get("success").Bool()
		inputValue := inputSuccess.Value.Get("value").Str()
		log.Printf("[注册 %d] JS输入结果: success=%v, value=%s", threadID, jsSuccess, inputValue)
	}

	// 如果JS输入失败，使用键盘输入
	if !jsSuccess {
		log.Printf("[注册 %d] JS输入失败，尝试键盘输入...", threadID)
		// 先清空输入框
		page.Keyboard.Press(input.ControlLeft)
		page.Keyboard.Type(input.KeyA)
		page.Keyboard.Release(input.ControlLeft)
		page.Keyboard.Press(input.Backspace)
		time.Sleep(100 * time.Millisecond)

		// 逐字符输入
		for _, char := range email {
			page.Keyboard.Type(input.Key(char))
			time.Sleep(20 * time.Millisecond)
		}
		log.Printf("[注册 %d] 键盘输入完成", threadID)
	}

	time.Sleep(500 * time.Millisecond)
	debugScreenshot(page, threadID, "02_email_input")

	// 触发 blur
	page.Eval(`() => {
		const inputs = document.querySelectorAll('input');
		if (inputs.length > 0) {
			inputs[0].blur();
		}
	}`)
	time.Sleep(500 * time.Millisecond)
	debugScreenshot(page, threadID, "03_before_submit")
	emailSubmitted := false
	for i := 0; i < 8; i++ {
		clickResult, _ := page.Eval(`() => {
			if (!document.body) return { clicked: false, reason: 'body_null' };
			
			const targets = ['继续', 'Next', '邮箱', 'Continue'];
			const elements = [
				...document.querySelectorAll('button'),
				...document.querySelectorAll('input[type="submit"]'),
				...document.querySelectorAll('div[role="button"]'),
				...document.querySelectorAll('span[role="button"]')
			];

			for (const element of elements) {
				if (!element) continue;
				const style = window.getComputedStyle(element);
				if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') continue;
				if (element.disabled) continue;

				const text = element.textContent ? element.textContent.trim() : '';
				if (targets.some(t => text.includes(t))) {
					element.click();
					return { clicked: true, text: text };
				}
			}
			return { clicked: false, reason: 'no_button' };
		}`)

		if clickResult != nil && clickResult.Value.Get("clicked").Bool() {
			emailSubmitted = true
			break
		}
		log.Printf("[注册 %d] 尝试 %d/8: 未找到按钮", threadID, i+1)
		time.Sleep(1 * time.Second)
	}
	if !emailSubmitted {
		result.Error = fmt.Errorf("找不到提交按钮")
		return result
	}
	// 等待页面跳转，最多等待15秒
	var needsVerification bool
	var pageTransitioned bool
	for waitCount := 0; waitCount < 15; waitCount++ {
		time.Sleep(1 * time.Second)

		// 检查页面是否已经离开邮箱输入页面
		transitionResult, _ := page.Eval(`() => {
			const pageText = document.body ? document.body.textContent : '';
			const emailInput = document.querySelector('input[type="email"]');
			const continueBtn = document.querySelector('button[jsname="LgbsSe"]');
			
			// 检查是否还在邮箱输入页面
			const stillOnEmailPage = (emailInput && emailInput.offsetParent !== null) || 
				(continueBtn && continueBtn.innerText && 
				 (continueBtn.innerText.includes('继续') || continueBtn.innerText.includes('Continue')));
			
			// 检查是否跳转到验证码页面
			const isVerifyPage = pageText.includes('验证') || pageText.includes('Verify') || 
				pageText.includes('输入代码') || pageText.includes('Enter code') ||
				pageText.includes('发送到') || pageText.includes('sent to');
			
			// 检查是否跳转到姓名页面
			const isNamePage = pageText.includes('姓氏') || pageText.includes('名字') || 
				pageText.includes('Full name') || pageText.includes('全名');
			
			// 检查错误
			const hasError = pageText.includes('出了点问题') || pageText.includes('Something went wrong') ||
				pageText.includes('无法创建') || pageText.includes('cannot create') ||
				pageText.includes('电话') || pageText.includes('Phone');
			
			return {
				stillOnEmailPage: stillOnEmailPage && !isVerifyPage && !isNamePage,
				isVerifyPage: isVerifyPage,
				isNamePage: isNamePage,
				hasError: hasError,
				errorText: hasError ? document.body.innerText.substring(0, 100) : ''
			};
		}`)

		if transitionResult != nil {
			if transitionResult.Value.Get("hasError").Bool() {
				result.Error = fmt.Errorf("页面显示错误: %s", transitionResult.Value.Get("errorText").String())
				log.Printf("[注册 %d] ❌ %v", threadID, result.Error)
				return result
			}

			if !transitionResult.Value.Get("stillOnEmailPage").Bool() {
				pageTransitioned = true
				needsVerification = transitionResult.Value.Get("isVerifyPage").Bool()
				isNamePage := transitionResult.Value.Get("isNamePage").Bool()
				log.Printf("[注册 %d] 页面已跳转: needsVerification=%v, isNamePage=%v", threadID, needsVerification, isNamePage)
				break
			}
		}

		if waitCount%3 == 2 {
			log.Printf("[注册 %d] 等待页面跳转... (%d/15秒)", threadID, waitCount+1)
		}
	}

	debugScreenshot(page, threadID, "04_after_submit")

	if !pageTransitioned {
		// 页面没有跳转，可能需要重新点击按钮
		log.Printf("[注册 %d] 页面未跳转，尝试重新点击按钮", threadID)
		page.Eval(`() => {
			const btn = document.querySelector('button[jsname="LgbsSe"]');
			if (btn) btn.click();
		}`)
		time.Sleep(3 * time.Second)
		needsVerification = true // 假设需要验证
	}

	// 再次检查页面状态
	checkResult, _ := page.Eval(`() => {
		const pageText = document.body ? document.body.textContent : '';
		
		// 检查常见错误
		if (pageText.includes('出了点问题') || pageText.includes('Something went wrong') ||
			pageText.includes('无法创建') || pageText.includes('cannot create') ||
			pageText.includes('不安全') || pageText.includes('secure') ||
			pageText.includes('电话') || pageText.includes('Phone') || pageText.includes('number')) {
			return { error: true, text: document.body.innerText.substring(0, 100) };
		}

		// 检查是否需要验证码
		if (pageText.includes('验证') || pageText.includes('Verify') || 
			pageText.includes('code') || pageText.includes('sent')) {
			return { needsVerification: true, isNamePage: false };
		}
		
		// 检查是否已经到了姓名页面
		if (pageText.includes('姓氏') || pageText.includes('名字') || 
			pageText.includes('Full name') || pageText.includes('全名')) {
			return { needsVerification: false, isNamePage: true };
		}
		
		return { needsVerification: true, isNamePage: false };
	}`)

	if checkResult != nil {
		if checkResult.Value.Get("error").Bool() {
			errText := checkResult.Value.Get("text").String()
			result.Error = fmt.Errorf("页面显示错误: %s...", errText)
			log.Printf("[注册 %d] ❌ %v", threadID, result.Error)
			return result
		}
		needsVerification = checkResult.Value.Get("needsVerification").Bool()
		isNamePage := checkResult.Value.Get("isNamePage").Bool()
		log.Printf("[注册 %d] 页面状态: needsVerification=%v, isNamePage=%v", threadID, needsVerification, isNamePage)
	} else {
		needsVerification = true
	}

	// 处理验证码
	if needsVerification {

		var emailContent *EmailContent
		maxWaitTime := 3 * time.Minute
		startTime := time.Now()
		clickCount := 0

		for time.Since(startTime) < maxWaitTime {
			// 尝试点击重发按钮
			clickResult, _ := page.Eval(`() => {
				// 精确匹配: <span jsname="V67aGc" class="YuMlnb-vQzf8d">重新发送验证码</span>
				const btn = document.querySelector('span[jsname="V67aGc"].YuMlnb-vQzf8d') ||
				            document.querySelector('span.YuMlnb-vQzf8d');
				
				if (btn && btn.textContent.includes('重新发送')) {
					btn.click();
					if (btn.parentElement) btn.parentElement.click();
					return {clicked: true};
				}
				return {clicked: false};
			}`)

			if clickResult != nil && clickResult.Value.Get("clicked").Bool() {
				clickCount++
				time.Sleep(1 * time.Second)
			}

			// 快速检查邮件
			emailContent, _ = getVerificationEmailQuick(email, 1, 1)
			if emailContent != nil {
				break
			}
		}

		if emailContent == nil {
			result.Error = fmt.Errorf("无法获取验证码邮件")
			return result
		}

		// 提取验证码
		code, err := extractVerificationCode(emailContent.Content)
		if err != nil {
			result.Error = err
			return result
		}

		// 等待验证码输入框
		time.Sleep(500 * time.Millisecond)
		log.Printf("[注册 %d] 准备输入验证码: %s", threadID, code)

		// 检查是否是OTP风格的多个输入框
		inputInfo, _ := page.Eval(`() => {
			// 检查标准input
			const inputs = document.querySelectorAll('input:not([type="hidden"])');
			const visibleInputs = Array.from(inputs).filter(i => i.offsetParent !== null);
			
			// 检查Google风格的OTP框（可能是div实现）
			const otpContainers = document.querySelectorAll('[data-otp-input], [class*="otp"], [class*="code-input"], [class*="verification"]');
			
			// 检查页面是否包含验证码相关文本
			const pageText = document.body ? document.body.innerText : '';
			const isVerifyPage = pageText.includes('验证码') || pageText.includes('verification') || 
				pageText.includes('verify') || window.location.href.includes('verify');
			const isOTP = (visibleInputs.length >= 4 && visibleInputs.length <= 8) || 
				(isVerifyPage && visibleInputs.length <= 2);
			
			return { 
				count: visibleInputs.length,
				isOTP: isOTP,
				isVerifyPage: isVerifyPage,
				url: window.location.href
			};
		}`)

		isOTP := false
		if inputInfo != nil {
			isOTP = inputInfo.Value.Get("isOTP").Bool()
			log.Printf("[注册 %d] 验证码输入框: count=%d, isOTP=%v", threadID,
				inputInfo.Value.Get("count").Int(), isOTP)
		}

		if isOTP {
			// OTP风格：先清空所有输入框，聚焦第一个，然后用键盘逐字符输入
			page.Eval(`() => {
				// 清空所有输入框
				const inputs = document.querySelectorAll('input:not([type="hidden"])');
				inputs.forEach(i => { i.value = ''; });
				
				// 尝试找到第一个OTP输入框并聚焦
				const visibleInputs = Array.from(inputs).filter(i => i.offsetParent !== null);
				if (visibleInputs.length > 0) {
					visibleInputs[0].click();
					visibleInputs[0].focus();
				}
			}`)
			time.Sleep(300 * time.Millisecond)

			// 逐字符输入验证码（每个字符之间稍微延迟，让页面自动跳转到下一个框）
			for i, char := range code {
				page.Keyboard.Type(input.Key(char))
				if i < len(code)-1 {
					time.Sleep(150 * time.Millisecond) // 给页面时间跳转到下一个输入框
				}
			}
		} else {
			// 单个输入框：直接输入
			page.Eval(`() => {
				const inputs = document.querySelectorAll('input:not([type="hidden"])');
				if (inputs.length > 0) {
					inputs[0].value = '';
					inputs[0].click();
					inputs[0].focus();
				}
			}`)
			time.Sleep(200 * time.Millisecond)
			safeType(page, code, 15)
		}

		time.Sleep(500 * time.Millisecond)

		for i := 0; i < 5; i++ {
			clickResult, _ := page.Eval(`() => {
				const targets = ['验证', 'Verify', '继续', 'Next', 'Continue'];
				const elements = [
					...document.querySelectorAll('button'),
					...document.querySelectorAll('input[type="submit"]'),
					...document.querySelectorAll('div[role="button"]')
				];

				for (const element of elements) {
					if (!element) continue;
					const style = window.getComputedStyle(element);
					if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') continue;
					if (element.disabled) continue;

					const text = element.textContent ? element.textContent.trim() : '';
					if (targets.some(t => text.includes(t))) {
						element.click();
						return { clicked: true, text: text };
					}
				}
				return { clicked: false };
			}`)

			if clickResult != nil && clickResult.Value.Get("clicked").Bool() {
				break
			}
			time.Sleep(1 * time.Second)
		}

		time.Sleep(2 * time.Second)
	}

	// 填写姓名
	fullName := generateRandomName()
	result.FullName = fullName
	log.Printf("[注册 %d] 准备输入姓名: %s", threadID, fullName)

	time.Sleep(500 * time.Millisecond)

	// 查找姓名输入框
	page.Eval(`() => {
		const selectors = [
			'input[name="fullName"]',
			'input[autocomplete="name"]',
			'input[type="text"]',
			'input:not([type="hidden"]):not([type="submit"]):not([type="email"])'
		];
		for (const sel of selectors) {
			const input = document.querySelector(sel);
			if (input && input.offsetParent !== null) {
				input.value = '';
				input.click();
				input.focus();
				return true;
			}
		}
		// 兜底
		const inputs = document.querySelectorAll('input:not([type="hidden"])');
		if (inputs.length > 0) {
			inputs[0].value = '';
			inputs[0].click();
			inputs[0].focus();
		}
		return false;
	}`)
	time.Sleep(200 * time.Millisecond)

	// 输入姓名 - 使用键盘输入
	for _, char := range fullName {
		page.Keyboard.Type(input.Key(char))
		time.Sleep(30 * time.Millisecond)
	}
	log.Printf("[注册 %d] 姓名输入完成", threadID)
	time.Sleep(500 * time.Millisecond)

	// 确认提交姓名
	confirmSubmitted := false
	for i := 0; i < 5; i++ {
		clickResult, _ := page.Eval(`() => {
			const targets = ['同意', 'Confirm', '继续', 'Next', 'Continue', 'I agree'];
			const elements = [
				...document.querySelectorAll('button'),
				...document.querySelectorAll('input[type="submit"]'),
				...document.querySelectorAll('div[role="button"]')
			];

			for (const element of elements) {
				if (!element) continue;
				const style = window.getComputedStyle(element);
				if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') continue;
				if (element.disabled) continue;

				const text = element.textContent ? element.textContent.trim() : '';
				if (targets.some(t => text.includes(t))) {
					element.click();
					return { clicked: true, text: text };
				}
			}

			// 备用：点击第一个可见按钮
			for (const element of elements) {
				if (element && element.offsetParent !== null && !element.disabled) {
					element.click();
					return { clicked: true, text: 'fallback' };
				}
			}
			return { clicked: false };
		}`)

		if clickResult != nil && clickResult.Value.Get("clicked").Bool() {
			confirmSubmitted = true
			break
		}
		time.Sleep(1000 * time.Millisecond)
	}

	if !confirmSubmitted {
		log.Printf("[注册 %d] ⚠️ 未能点击确认按钮，尝试继续", threadID)
	}

	time.Sleep(3 * time.Second)

	// 等待页面稳定
	page.WaitLoad()
	time.Sleep(2 * time.Second)

	// 处理额外步骤（主要是复选框）
	handleAdditionalSteps(page, threadID)

	// 检查并处理管理创建页面
	checkAndHandleAdminPage(page, threadID)

	// 等待更多可能的跳转
	time.Sleep(3 * time.Second)

	// 尝试多次点击可能出现的额外按钮
	for i := 0; i < 15; i++ {
		time.Sleep(2 * time.Second)

		// 尝试点击可能出现的额外按钮
		page.Eval(`() => {
			const buttons = document.querySelectorAll('button');
			for (const button of buttons) {
				if (!button) continue;
				const text = button.textContent || '';
				if (text.includes('同意') || text.includes('Confirm') || text.includes('继续') || 
					text.includes('Next') || text.includes('I agree')) {
					if (button.offsetParent !== null && !button.disabled) {
						button.click();
						return true;
					}
				}
			}
			return false;
		}`)

		// 从 URL 提取信息
		info, _ := page.Info()
		if info != nil {
			currentURL := info.URL
			if m := regexp.MustCompile(`/cid/([a-f0-9-]+)`).FindStringSubmatch(currentURL); len(m) > 1 && configID == "" {
				configID = m[1]
				log.Printf("[注册 %d] 从URL提取 configId: %s", threadID, configID)
			}
			if m := regexp.MustCompile(`[?&]csesidx=(\d+)`).FindStringSubmatch(currentURL); len(m) > 1 && csesidx == "" {
				csesidx = m[1]
				log.Printf("[注册 %d] 从URL提取 csesidx: %s", threadID, csesidx)
			}
		}

		if authorization != "" {
			break
		}
	}

	// 增强的 Authorization 获取逻辑
	if authorization == "" {
		log.Printf("[注册 %d] 仍未获取到 Authorization，尝试更多方法...", threadID)

		// 尝试刷新页面
		page.Reload()
		page.WaitLoad()
		time.Sleep(3 * time.Second)

		// 尝试从 localStorage 获取
		localStorageAuth, _ := page.Eval(`() => {
			return localStorage.getItem('Authorization') || 
				   localStorage.getItem('authorization') ||
				   localStorage.getItem('auth_token') ||
				   localStorage.getItem('token');
		}`)

		if localStorageAuth != nil && localStorageAuth.Value.String() != "" {
			authorization = localStorageAuth.Value.String()
			log.Printf("[注册 %d] 从 localStorage 获取 Authorization", threadID)
		}

		// 从页面源代码中提取
		pageContent, _ := page.Eval(`() => document.body ? document.body.innerHTML : ''`)
		if pageContent != nil && pageContent.Value.String() != "" {
			content := pageContent.Value.String()
			re := regexp.MustCompile(`"authorization"\s*:\s*"([^"]+)"`)
			if matches := re.FindStringSubmatch(content); len(matches) > 1 {
				authorization = matches[1]
				log.Printf("[注册 %d] 从页面内容提取 Authorization", threadID)
			}
		}

		// 从当前 URL 中提取
		info, _ := page.Info()
		if info != nil {
			currentURL := info.URL
			re := regexp.MustCompile(`[?&](?:token|auth)=([^&]+)`)
			if matches := re.FindStringSubmatch(currentURL); len(matches) > 1 {
				authorization = matches[1]
				log.Printf("[注册 %d] 从 URL 提取 Authorization", threadID)
			}
		}
	}

	if authorization == "" {
		result.Error = fmt.Errorf("未能获取 Authorization")
		return result
	}
	var resultCookies []pool.Cookie
	cookieMap := make(map[string]bool)

	// 获取当前页面所有 cookie
	cookies, _ := page.Cookies(nil)
	for _, c := range cookies {
		key := c.Name + "|" + c.Domain
		if !cookieMap[key] {
			cookieMap[key] = true
			resultCookies = append(resultCookies, pool.Cookie{
				Name:   c.Name,
				Value:  c.Value,
				Domain: c.Domain,
			})
		}
	}

	// 尝试从特定域名获取更多 cookie
	domains := []string{
		"https://business.gemini.google",
		"https://gemini.google",
		"https://accounts.google.com",
	}
	for _, domain := range domains {
		domainCookies, err := page.Cookies([]string{domain})
		if err == nil {
			for _, c := range domainCookies {
				key := c.Name + "|" + c.Domain
				if !cookieMap[key] {
					cookieMap[key] = true
					resultCookies = append(resultCookies, pool.Cookie{
						Name:   c.Name,
						Value:  c.Value,
						Domain: c.Domain,
					})
				}
			}
		}
	}

	log.Printf("[注册 %d] 获取到 %d 个 Cookie", threadID, len(resultCookies))

	result.Success = true
	result.Authorization = authorization
	result.Cookies = resultCookies
	result.ConfigID = configID
	result.CSESIDX = csesidx

	log.Printf("[注册 %d] ✅ 注册成功: %s", threadID, email)
	return result
}

// SaveBrowserRegisterResult 保存注册结果
func SaveBrowserRegisterResult(result *BrowserRegisterResult, dataDir string) error {
	if !result.Success {
		return result.Error
	}

	data := pool.AccountData{
		Email:         result.Email,
		FullName:      result.FullName,
		Authorization: result.Authorization,
		Cookies:       result.Cookies,
		ConfigID:      result.ConfigID,
		CSESIDX:       result.CSESIDX,
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	filename := filepath.Join(dataDir, fmt.Sprintf("%s.json", result.Email))
	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	return nil
}

// BrowserRefreshResult Cookie刷新结果
type BrowserRefreshResult struct {
	Success         bool
	SecureCookies   []pool.Cookie
	ConfigID        string
	CSESIDX         string
	Authorization   string
	ResponseHeaders map[string]string // 捕获的响应头
	NewCookies      []pool.Cookie     // 从响应头提取的新Cookie
	Error           error
}

func RefreshCookieWithBrowser(acc *pool.Account, headless bool, proxy string) *BrowserRefreshResult {
	result := &BrowserRefreshResult{}
	email := acc.Data.Email

	defer func() {
		if r := recover(); r != nil {
			result.Error = fmt.Errorf("panic: %v", r)
		}
	}()

	// 启动浏览器
	l := launcher.New()
	systemBrowsers := []string{
		"/usr/bin/google-chrome", "/usr/bin/google-chrome-stable",
		"/usr/bin/chromium", "/usr/bin/chromium-browser",
		"/snap/bin/chromium", "/opt/google/chrome/chrome",
		"/usr/lib/chromium/chromium",
		"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
	}
	for _, path := range systemBrowsers {
		if _, err := os.Stat(path); err == nil {
			l = l.Bin(path)
			break
		}
	}

	l = l.Headless(headless).
		Set("no-sandbox").
		Set("disable-setuid-sandbox").
		Set("disable-dev-shm-usage").
		Set("disable-gpu").
		Set("disable-blink-features", "AutomationControlled").
		Set("window-size", "1280,800")

	if proxy != "" {
		l = l.Proxy(proxy)
	}

	url, err := l.Launch()
	if err != nil {
		result.Error = fmt.Errorf("启动浏览器失败: %w", err)
		return result
	}

	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		result.Error = fmt.Errorf("连接浏览器失败: %w", err)
		return result
	}
	defer browser.Close()

	browser = browser.Timeout(120 * time.Second)

	pages, err := browser.Pages()
	if err != nil {
		result.Error = fmt.Errorf("获取页面列表失败: %w", err)
		return result
	}

	var page *rod.Page
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = browser.Page(proto.TargetCreateTarget{URL: "about:blank"})
		if err != nil {
			result.Error = fmt.Errorf("创建新页面失败: %w", err)
			return result
		}
	}

	if page == nil {
		result.Error = fmt.Errorf("无法获取或创建浏览器页面")
		return result
	}

	if err := page.SetViewport(&proto.EmulationSetDeviceMetricsOverride{
		Width:  1280,
		Height: 800,
	}); err != nil {
		log.Printf("[Cookie刷新] ⚠️ 设置视口失败: %v", err)
	}

	if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	}); err != nil {
		log.Printf("[Cookie刷新] ⚠️ 设置 User-Agent 失败: %v", err)
	}

	var authorization string
	var configID, csesidx string
	var responseHeadersMu sync.Mutex
	responseHeaders := make(map[string]string)
	var newCookiesFromResponse []pool.Cookie
	go page.EachEvent(func(e *proto.NetworkResponseReceived) {
		responseHeadersMu.Lock()
		defer responseHeadersMu.Unlock()
		headers := e.Response.Headers
		importantKeys := []string{"set-cookie", "Set-Cookie", "authorization", "Authorization",
			"x-goog-authenticated-user", "X-Goog-Authenticated-User"}

		for _, key := range importantKeys {
			if val, ok := headers[key]; ok {
				str := val.Str()
				if str == "" {
					continue
				}
				responseHeaders[key] = str
				// 解析 Set-Cookie
				if strings.ToLower(key) == "set-cookie" {
					parts := strings.Split(str, ";")
					if len(parts) > 0 {
						nv := strings.SplitN(parts[0], "=", 2)
						if len(nv) == 2 {
							newCookiesFromResponse = append(newCookiesFromResponse, pool.Cookie{
								Name:   strings.TrimSpace(nv[0]),
								Value:  strings.TrimSpace(nv[1]),
								Domain: ".gemini.google",
							})
						}
					}
				}
			}
		}
	})()

	go page.EachEvent(func(e *proto.NetworkRequestWillBeSent) {
		if auth, ok := e.Request.Headers["authorization"]; ok {
			if authStr := auth.String(); authStr != "" {
				authorization = authStr
			}
		}
		reqURL := e.Request.URL
		if m := regexp.MustCompile(`/cid/([a-f0-9-]+)`).FindStringSubmatch(reqURL); len(m) > 1 && configID == "" {
			configID = m[1]
		}
		if m := regexp.MustCompile(`[?&]csesidx=(\d+)`).FindStringSubmatch(reqURL); len(m) > 1 && csesidx == "" {
			csesidx = m[1]
		}
	})()

	// 导航到目标页面
	targetURL := "https://business.gemini.google/"
	page.Navigate(targetURL)
	page.WaitLoad()
	time.Sleep(2 * time.Second)

	// 检查页面状态
	info, _ := page.Info()
	currentURL := ""
	if info != nil {
		currentURL = info.URL
	}
	initialEmailCount := 0
	maxCodeRetries := 3 // 验证码重试次数（必须在goto之前声明）

	// 检查是否已经登录成功（有authorization）
	if authorization != "" {
		log.Printf("[Cookie刷新] [%s] Cookie有效，已自动登录", email)
		goto extractResult
	}

	// 获取实际邮件数量
	initialEmailCount = getEmailCount(email)

	// 检查是否在登录页面需要输入邮箱
	if _, err := page.Timeout(5 * time.Second).Element("input"); err == nil {

		// 输入邮箱 - 先清空再输入
		time.Sleep(500 * time.Millisecond)
		page.Eval(`() => {
			const inputs = document.querySelectorAll('input');
			if (inputs.length > 0) {
				inputs[0].value = '';
				inputs[0].click();
				inputs[0].focus();
			}
		}`)
		time.Sleep(300 * time.Millisecond)
		safeType(page, email, 30)
		time.Sleep(500 * time.Millisecond)
		page.Eval(`() => {
			const inputs = document.querySelectorAll('input');
			if (inputs.length > 0) { inputs[0].blur(); }
		}`)
		time.Sleep(500 * time.Millisecond)

		// 点击继续按钮
		for i := 0; i < 5; i++ {
			clickResult, _ := page.Eval(`() => {
				const targets = ['继续', 'Next', 'Continue', '邮箱'];
				const elements = [...document.querySelectorAll('button'), ...document.querySelectorAll('div[role="button"]')];
				for (const el of elements) {
					if (!el || el.disabled) continue;
					const style = window.getComputedStyle(el);
					if (style.display === 'none' || style.visibility === 'hidden') continue;
					const text = el.textContent ? el.textContent.trim() : '';
					if (targets.some(t => text.includes(t))) { el.click(); return {clicked:true}; }
				}
				return {clicked:false};
			}`)
			if clickResult != nil && clickResult.Value.Get("clicked").Bool() {
				break
			}
			time.Sleep(1 * time.Second)
		}
		time.Sleep(2 * time.Second)
	}
	time.Sleep(3 * time.Second)

	// 验证码重试循环
	for codeRetry := 0; codeRetry < maxCodeRetries; codeRetry++ {
		if codeRetry > 0 {
			log.Printf("[Cookie刷新] [%s] 验证码验证失败，重试 %d/%d", email, codeRetry+1, maxCodeRetries)
			// 点击"重新发送验证码"按钮
			page.Eval(`() => {
				const links = document.querySelectorAll('a, span, button');
				for (const el of links) {
					const text = el.textContent || '';
					if (text.includes('重新发送') || text.includes('Resend')) {
						el.click();
						return true;
					}
				}
				return false;
			}`)
			time.Sleep(2 * time.Second)
			// 更新邮件计数基准
			initialEmailCount = getEmailCount(email)
		}

		var emailContent *EmailContent
		maxWaitTime := 3 * time.Minute
		startTime := time.Now()

		for time.Since(startTime) < maxWaitTime {
			// 快速检查新邮件（只接受数量增加的情况）
			emailContent, _ = getVerificationEmailAfter(email, 1, 1, initialEmailCount)
			if emailContent != nil {
				break
			}
			time.Sleep(2 * time.Second)
		}

		if emailContent == nil {
			result.Error = fmt.Errorf("无法获取验证码邮件")
			return result
		}

		// 提取验证码
		code, err := extractVerificationCode(emailContent.Content)
		if err != nil {
			continue // 重试
		}

		// 输入验证码
		time.Sleep(500 * time.Millisecond)
		page.Eval(`() => {
			const inputs = document.querySelectorAll('input');
			for (const inp of inputs) { inp.value = ''; }
			if (inputs.length > 0) { inputs[0].click(); inputs[0].focus(); }
		}`)
		time.Sleep(300 * time.Millisecond)
		safeType(page, code, 30)
		time.Sleep(500 * time.Millisecond)

		// 点击验证按钮
		for i := 0; i < 5; i++ {
			clickResult, _ := page.Eval(`() => {
				const targets = ['验证', 'Verify', '继续', 'Next', 'Continue'];
				const els = [...document.querySelectorAll('button'), ...document.querySelectorAll('div[role="button"]')];
				for (const el of els) {
					if (!el || el.disabled) continue;
					const style = window.getComputedStyle(el);
					if (style.display === 'none' || style.visibility === 'hidden') continue;
					const text = el.textContent ? el.textContent.trim() : '';
					if (targets.some(t => text.includes(t))) { el.click(); return {clicked:true}; }
				}
				return {clicked:false};
			}`)
			if clickResult != nil && clickResult.Value.Get("clicked").Bool() {
				break
			}
			time.Sleep(1 * time.Second)
		}
		time.Sleep(2 * time.Second)

		// 检测验证码错误
		hasError, _ := page.Eval(`() => {
			const text = document.body.innerText || '';
			return text.includes('验证码有误') || text.includes('incorrect') || text.includes('wrong code') || text.includes('请重试');
		}`)
		if hasError != nil && hasError.Value.Bool() {
			continue // 重试
		}

		// 验证成功，跳出重试循环
		break
	}
	for i := 0; i < 15; i++ {
		time.Sleep(2 * time.Second)

		// 点击可能出现的确认按钮
		page.Eval(`() => {
			const btns = document.querySelectorAll('button');
			for (const btn of btns) {
				const text = btn.textContent || '';
				if ((text.includes('同意') || text.includes('Confirm') || text.includes('继续') || text.includes('I agree')) && btn.offsetParent !== null && !btn.disabled) {
					btn.click(); return true;
				}
			}
			return false;
		}`)

		// 从URL提取信息
		info, _ := page.Info()
		if info != nil {
			if m := regexp.MustCompile(`/cid/([a-f0-9-]+)`).FindStringSubmatch(info.URL); len(m) > 1 && configID == "" {
				configID = m[1]
			}
			if m := regexp.MustCompile(`[?&]csesidx=(\d+)`).FindStringSubmatch(info.URL); len(m) > 1 && csesidx == "" {
				csesidx = m[1]
			}
		}

		if authorization != "" {
			break
		}
	}

extractResult:
	if authorization == "" {
		result.Error = fmt.Errorf("未能获取 Authorization")
		return result
	}
	cookies, _ := page.Cookies(nil)
	cookieMap := make(map[string]pool.Cookie)
	for _, c := range acc.Data.GetAllCookies() {
		cookieMap[c.Name] = c
	}

	for _, c := range cookies {
		cookieMap[c.Name] = pool.Cookie{
			Name:   c.Name,
			Value:  c.Value,
			Domain: c.Domain,
		}
	}
	responseHeadersMu.Lock()
	for _, c := range newCookiesFromResponse {
		cookieMap[c.Name] = c
	}
	// 复制响应头
	result.ResponseHeaders = make(map[string]string)
	for k, v := range responseHeaders {
		result.ResponseHeaders[k] = v
	}
	result.NewCookies = newCookiesFromResponse
	responseHeadersMu.Unlock()
	var resultCookies []pool.Cookie
	for _, c := range cookieMap {
		resultCookies = append(resultCookies, c)
	}
	info, _ = page.Info()
	if info != nil {
		currentURL = info.URL
		if m := regexp.MustCompile(`/cid/([a-f0-9-]+)`).FindStringSubmatch(currentURL); len(m) > 1 && configID == "" {
			configID = m[1]
		}
		if m := regexp.MustCompile(`[?&]csesidx=(\d+)`).FindStringSubmatch(currentURL); len(m) > 1 && csesidx == "" {
			csesidx = m[1]
		}
	}

	result.Success = true
	result.Authorization = authorization
	result.SecureCookies = resultCookies
	result.ConfigID = configID
	result.CSESIDX = csesidx

	log.Printf("[Cookie刷新] ✅ [%s] 刷新成功", email)
	return result
}
func NativeRegisterWorker(id int, dataDirAbs string) {
	time.Sleep(time.Duration(id) * 3 * time.Second)

	for atomic.LoadInt32(&IsRegistering) == 1 {
		if pool.Pool.TotalCount() >= TargetCount {
			return
		}

		logger.Debug("[注册线程 %d] 启动注册任务", id)

		result := RunBrowserRegister(Headless, Proxy, id)

		if result.Success {
			if err := SaveBrowserRegisterResult(result, dataDirAbs); err != nil {
				logger.Warn("[注册线程 %d] ⚠️ 保存失败: %v", id, err)
				Stats.AddFailed(err.Error())
			} else {
				Stats.AddSuccess()
				pool.Pool.Load(DataDir)
			}
		} else {
			errMsg := "未知错误"
			if result.Error != nil {
				errMsg = result.Error.Error()
			}
			logger.Warn("[注册线程 %d] ❌ 注册失败: %s", id, errMsg)
			Stats.AddFailed(errMsg)

			if strings.Contains(errMsg, "频繁") || strings.Contains(errMsg, "rate") ||
				strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "连接") {
				waitTime := 10 + id*2
				logger.Debug("[注册线程 %d] ⏳ 等待 %d 秒后重试...", id, waitTime)
				time.Sleep(time.Duration(waitTime) * time.Second)
			} else {
				time.Sleep(3 * time.Second)
			}
		}
	}
	logger.Debug("[注册线程 %d] 停止", id)
}
