// Copyright 2022 Juan Pablo Tosso and the OWASP Coraza contributors
// SPDX-License-Identifier: Apache-2.0

// tinygo does not support net.http so this package is not needed for it
//go:build !tinygo
// +build !tinygo

package http

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"

	"github.com/corazawaf/coraza/v3"
	"github.com/corazawaf/coraza/v3/internal/corazawaf"
	"github.com/corazawaf/coraza/v3/internal/seclang"
	"github.com/corazawaf/coraza/v3/loggers"
	"github.com/corazawaf/coraza/v3/macro"
	"github.com/corazawaf/coraza/v3/types"
)

func TestProcessRequest(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://www.coraza.io/test", strings.NewReader("test=456"))
	waf := corazawaf.NewWAF()
	tx := waf.NewTransaction()
	if _, err := processRequest(tx, req); err != nil {
		t.Fatal(err)
	}
	if tx.Variables().RequestMethod().String() != "POST" {
		t.Fatal("failed to set request from request object")
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessRequestEngineOff(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://www.coraza.io/test", strings.NewReader("test=456"))
	waf := corazawaf.NewWAF()
	waf.RuleEngine = types.RuleEngineOff
	tx := waf.NewTransaction()
	if _, err := processRequest(tx, req); err != nil {
		t.Fatal(err)
	}
	if tx.Variables().RequestMethod().String() != "POST" {
		t.Fatal("failed to set request from request object")
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessRequestMultipart(t *testing.T) {
	waf := corazawaf.NewWAF()
	waf.RequestBodyAccess = true

	tx := waf.NewTransaction()
	tx.RequestBodyAccess = true

	req := createMultipartRequest(t)

	if _, err := processRequest(tx, req); err != nil {
		t.Fatal(err)
	}

	if req.Body == nil {
		t.Error("failed to process multipart request: nil body")
	}
	defer req.Body.Close()

	reader := bufio.NewReader(req.Body)
	if _, err := reader.ReadString('\n'); err != nil {
		t.Errorf("failed to read multipart request: %s", err.Error())
	}

	if err := tx.Close(); err != nil {
		t.Errorf("failed to close the transaction: %s", err.Error())
	}
}

func createMultipartRequest(t *testing.T) *http.Request {
	t.Helper()

	metadata := `{"name": "photo-sample.jpeg"}`
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	metadataHeader := textproto.MIMEHeader{}
	metadataHeader.Set("Content-Type", "application/json; charset=UTF-8")

	part, err := writer.CreatePart(metadataHeader)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte(metadata))

	mediaHeader := textproto.MIMEHeader{}
	mediaHeader.Set("Content-Type", "image/jpeg")

	mediaPart, err := writer.CreatePart(mediaHeader)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(mediaPart, bytes.NewReader([]byte{255, 1, 2}))

	writer.Close()

	req, err := http.NewRequest("POST", "/some", body)
	if err != nil {
		t.Fatal(err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Content-Length", fmt.Sprintf("%d", body.Len()))

	return req
}

// from issue https://github.com/corazawaf/coraza/issues/159 @zpeasystart
func TestDirectiveSecAuditLog(t *testing.T) {
	waf := corazawaf.NewWAF()
	p := seclang.NewParser(waf)
	if err := p.FromString(`
	SecRule REQUEST_FILENAME "@unconditionalMatch" "id:100, phase:2, t:none, log, setvar:'tx.count=+1',chain"
	SecRule ARGS:username "@unconditionalMatch" "t:none, setvar:'tx.count=+2',chain"
	SecRule ARGS:password "@unconditionalMatch" "t:none, setvar:'tx.count=+3'"
		`); err != nil {
		t.Error(err)
	}
	tx := waf.NewTransaction()
	defer tx.Close()
	tx.RequestBodyAccess = true
	tx.ForceRequestBodyVariable = true
	// request
	rdata := []string{
		"POST /login HTTP/1.1",
		"Accept: */*",
		"Accept-Encoding: gzip, deflate",
		"Connection: close",
		"Origin: http://test.com",
		"User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.77 Safari/537.36",
		"Content-Type: application/x-www-form-urlencoded; charset=UTF-8",
		"Referer: http://somehost.com/login.jsp",
		"X-Requested-With: XMLHttpRequest",
		"Content-Length: 59",
		"Accept-Language: zh-CN,zh;q=0.9",
		"",
		"username=root&password=123&rememberMe=on&time=1644979180757",
	}
	data := bytes.NewBuffer([]byte(strings.Join(rdata, "\r\n")))
	req, err := http.ReadRequest(bufio.NewReader(data))
	if err != nil {
		t.Errorf("Description HTTP request parsing failed")
	}

	_, err = processRequest(tx, req)
	if err != nil {
		t.Errorf("Failed to load the HTTP request")
	}

	rulesCounter := 0
	r := waf.Rules.FindByID(100)
	for r != nil {
		rulesCounter++
		r = r.Chain
	}
	if want, have := 3, rulesCounter; want != have {
		t.Errorf("failed to compile multiple chains, want: %d, have: %d", want, have)
	}

	m, err := macro.NewMacro("%{tx.count}")
	if err != nil {
		t.Fatalf("failed to initialize the macro: %v", err)
	}

	txCount, _ := strconv.Atoi(m.Expand(tx))
	if want, have := 6, txCount; want != have {
		t.Errorf("incorrect counter, want %d, have %d", want, have)
	}
}

func errLogger(t *testing.T) func(rule types.MatchedRule) {
	return func(rule types.MatchedRule) {
		t.Log(rule.ErrorLog(0))
	}
}

type debugLogger struct {
	t *testing.T
}

func (l *debugLogger) Info(message string, args ...interface{}) { l.t.Logf(message, args...) }

func (l *debugLogger) Warn(message string, args ...interface{}) { l.t.Logf(message, args...) }

func (l *debugLogger) Error(message string, args ...interface{}) { l.t.Logf(message, args...) }

func (l *debugLogger) Debug(message string, args ...interface{}) { l.t.Logf(message, args...) }

func (l *debugLogger) Trace(message string, args ...interface{}) { l.t.Logf(message, args...) }

func (l *debugLogger) SetLevel(level loggers.LogLevel) {
	l.t.Logf("Setting level to %q", level.String())
}

func (l *debugLogger) SetOutput(w io.WriteCloser) {
	l.t.Log("ignoring SecDebugLog directive, debug logs are always routed to proxy logs")
}

type httpTest struct {
	http2            bool
	reqURI           string
	reqBody          string
	echoReqBody      bool
	reqBodyLimit     int
	respHeaders      map[string]string
	respBody         string
	expectedProto    string
	expectedStatus   int
	expectedRespBody string
}

func TestHttpServer(t *testing.T) {
	tests := map[string]httpTest{
		"no blocking": {
			reqURI:         "/hello",
			expectedProto:  "HTTP/1.1",
			expectedStatus: 201,
		},
		"no blocking HTTP/2": {
			http2:          true,
			reqURI:         "/hello",
			expectedProto:  "HTTP/2.0",
			expectedStatus: 201,
		},
		"args blocking": {
			reqURI:         "/hello?id=0",
			expectedProto:  "HTTP/1.1",
			expectedStatus: 403,
		},
		"request body blocking": {
			reqURI:         "/hello",
			reqBody:        "eval('cat /etc/passwd')",
			expectedProto:  "HTTP/1.1",
			expectedStatus: 403,
		},
		"request body larger than limit": {
			reqURI:      "/hello",
			reqBody:     "eval('cat /etc/passwd')",
			echoReqBody: true,
			// Coraza only sees eva, not eval
			reqBodyLimit:     3,
			expectedProto:    "HTTP/1.1",
			expectedStatus:   201,
			expectedRespBody: "eval('cat /etc/passwd')",
		},
		"response headers blocking": {
			reqURI:         "/hello",
			respHeaders:    map[string]string{"foo": "bar"},
			expectedProto:  "HTTP/1.1",
			expectedStatus: 401,
		},
		"response body not blocking": {
			reqURI:           "/hello",
			respBody:         "true negative response body",
			expectedProto:    "HTTP/1.1",
			expectedStatus:   201,
			expectedRespBody: "true negative response body",
		},
		"response body blocking": {
			reqURI:           "/hello",
			respBody:         "password=xxxx",
			expectedProto:    "HTTP/1.1",
			expectedStatus:   403,
			expectedRespBody: "", // blocking at response body phase means returning it empty
		},
	}

	// Perform tests
	for name, tCase := range tests {
		t.Run(name, func(t *testing.T) {
			conf := coraza.NewWAFConfig().
				WithDirectives(`
		# This is a comment
		SecDebugLogLevel 5
		SecRequestBodyAccess On
		SecResponseBodyAccess On
		SecResponseBodyMimeType text/plain
		SecRule ARGS:id "@eq 0" "id:1, phase:1,deny, status:403,msg:'Invalid id',log,auditlog"
		SecRule REQUEST_BODY "@contains eval" "id:100, phase:2,deny, status:403,msg:'Invalid request body',log,auditlog"
		SecRule RESPONSE_HEADERS:Foo "@pm bar" "id:199,phase:3,deny,t:lowercase,deny, status:401,msg:'Invalid response header',log,auditlog"
		SecRule RESPONSE_BODY "@contains password" "id:200, phase:4,deny, status:403,msg:'Invalid response body',log,auditlog"
	`).WithErrorCallback(errLogger(t)).WithDebugLogger(&debugLogger{t: t})
			if l := tCase.reqBodyLimit; l > 0 {
				conf = conf.WithRequestBodyAccess(coraza.NewRequestBodyConfig().WithLimit(l).WithInMemoryLimit(l))
			}
			waf, err := coraza.NewWAF(conf)
			if err != nil {
				t.Fatal(err)
			}
			runAgainstWaf(t, tCase, waf)
		})
	}
}

func TestHttpServerWithRuleEngineOff(t *testing.T) {
	tests := map[string]httpTest{
		"no blocking true negative": {
			reqURI:           "/hello",
			expectedProto:    "HTTP/1.1",
			expectedStatus:   201,
			respBody:         "Hello!",
			expectedRespBody: "Hello!",
		},
		"no blocking true positive header phase": {
			reqURI:           "/hello?id=0",
			expectedProto:    "HTTP/1.1",
			expectedStatus:   201,
			respBody:         "Downstream works!",
			expectedRespBody: "Downstream works!",
		},
		"no blocking true positive body phase": {
			reqURI:           "/hello",
			reqBody:          "eval('cat /etc/passwd')",
			expectedProto:    "HTTP/1.1",
			expectedStatus:   201,
			respBody:         "Waf is Off!",
			expectedRespBody: "Waf is Off!",
		},
	}
	// Perform tests
	for name, tCase := range tests {
		t.Run(name, func(t *testing.T) {
			waf, err := coraza.NewWAF(coraza.NewWAFConfig().
				WithDirectives(`
			SecRuleEngine Off
			SecRequestBodyAccess On
			SecRule ARGS:id "@eq 0" "id:1, phase:1,deny, status:403,msg:'Invalid id',log,auditlog"
			SecRule REQUEST_BODY "@contains eval" "id:100, phase:2,deny, status:403,msg:'Invalid request body',log,auditlog"
			`).WithErrorCallback(errLogger(t)).WithDebugLogger(&debugLogger{t: t}))
			if err != nil {
				t.Fatal(err)
			}
			runAgainstWaf(t, tCase, waf)
		})
	}
}

func runAgainstWaf(t *testing.T, tCase httpTest, waf coraza.WAF) {
	t.Helper()
	serverErrC := make(chan error, 1)
	defer close(serverErrC)

	// Spin up the test server
	ts := httptest.NewUnstartedServer(WrapHandler(waf, t.Logf, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if want, have := tCase.expectedProto, req.Proto; want != have {
			t.Errorf("unexpected proto, want: %s, have: %s", want, have)
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Header().Add("coraza-middleware", "true")
		for k, v := range tCase.respHeaders {
			w.Header().Set(k, v)
		}
		w.WriteHeader(201)
		if tCase.echoReqBody {
			buf, err := io.ReadAll(req.Body)
			if err != nil {
				serverErrC <- err
			}
			if _, err := w.Write(buf); err != nil {
				serverErrC <- err
			}
		} else {
			if _, err := w.Write([]byte(tCase.respBody)); err != nil {
				serverErrC <- err
			}
		}
	})))
	if tCase.http2 {
		ts.EnableHTTP2 = true
		ts.StartTLS()
	} else {
		ts.Start()
	}
	defer ts.Close()

	var reqBody io.Reader
	if tCase.reqBody != "" {
		reqBody = strings.NewReader(tCase.reqBody)
	}
	req, _ := http.NewRequest("POST", ts.URL+tCase.reqURI, reqBody)
	// TODO(jcchavezs): Fix it once the discussion in https://github.com/corazawaf/coraza/issues/438 is settled
	req.Header.Add("content-type", "application/x-www-form-urlencoded")
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("unexpected error when performing the request: %v", err)
	}

	if want, have := tCase.expectedStatus, res.StatusCode; want != have {
		t.Errorf("unexpected status code, want: %d, have: %d", want, have)
	}

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("unexpected error when reading the response body: %v", err)
	}

	if want, have := tCase.expectedRespBody, string(resBody); want != have {
		t.Errorf("unexpected response body, want: %q, have %q", want, have)
	}

	err = res.Body.Close()
	if err != nil {
		t.Errorf("failed to close the body: %v", err)
	}

	select {
	case err = <-serverErrC:
		t.Errorf("unexpected error from server when writing response body: %v", err)
	default:
		return
	}
}

func TestObtainStatusCodeFromInterruptionOrDefault(t *testing.T) {
	tCases := map[string]struct {
		interruptionCode   int
		interruptionAction string
		defaultCode        int
		expectedCode       int
	}{
		"action deny with no code": {
			interruptionAction: "deny",
			expectedCode:       503,
		},
		"action deny with code": {
			interruptionAction: "deny",
			interruptionCode:   202,
			expectedCode:       202,
		},
		"default code": {
			defaultCode:  204,
			expectedCode: 204,
		},
	}

	for name, tCase := range tCases {
		t.Run(name, func(t *testing.T) {
			want := tCase.expectedCode
			have := obtainStatusCodeFromInterruptionOrDefault(&types.Interruption{
				Status: tCase.interruptionCode,
				Action: tCase.interruptionAction,
			}, tCase.defaultCode)
			if want != have {
				t.Errorf("unexpected status code, want %d, have %d", want, have)
			}
		})
	}
}