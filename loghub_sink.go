package log

import (
	"bytes"
	"io/ioutil"
	stdlog "log"
  "os"
	"net/http"
	"runtime/debug"
)

type LoghubWriter struct {
	client *http.Client
}

var loghubEndpoint = "http://loghub:8000/log"

func init() {
	if os.Getenv("INSIDE_DOCKER") == "" {
		loghubEndpoint = "http://127.0.0.1:8000/log"
	}
}

func (w *LoghubWriter) Write(line []byte) (n int, err error) {
	buf := new(bytes.Buffer)
	n, err = buf.Write(line)
	go func() {
		defer func() {
			err := recover()
			if err != nil {
				stdlog.Printf("[LOGHUB][HTTP] panic: %+v\n", err)
				debug.PrintStack()
			}
		}()
		resp, err := w.client.Post(loghubEndpoint, "text/plain", buf)
		if err != nil {
			stdlog.Printf("[LOGHUB][HTTP][pre-send] error: ['%s']\n", err)
		} else if resp.StatusCode != 200 {
			errReply, _ := ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			stdlog.Printf("[LOGHUB][HTTP][<- %d] error: ['%s']\n",
				resp.StatusCode, errReply)
		}
	}()
	return n, err
}
