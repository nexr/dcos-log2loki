package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	l "github.com/minyk/dcos-log2loki/logproto"
	t "github.com/minyk/dcos-log2loki/types"
	"io"
	"net/http"
	"sort"

	"github.com/r3labs/sse"
	"os"
	"strings"
	"time"
)

var (
	loggingPrefix string
	exist         = false
	dcosLogAPI    string
	logger        = level.NewFilter(log.With(log.NewJSONLogger(os.Stdout), "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller), level.AllowError())
	debug         = level.Debug(logger)
	info          = level.Info(logger)
	errorl        = level.Error(logger)

	lokiURL        string
	lokiPort       string
	internalBuffer *time.Duration
)

type logCollection []*t.DcosLog

var collection = make(logCollection, 0, 1000)

func main() {

	// move scrap config
	loggingPrefix, exist = os.LookupEnv("LOGGING_SERVICE_PREFIX")
	if !exist {
		loggingPrefix = "/"
	}

	//// move to scrap config
	dcosLogAPI, exist = os.LookupEnv("DCOS_LOG_API")
	if !exist {
		dcosLogAPI = "http://localhost:61001/system/v1/logs/v1/stream/?skip_prev=10"
	}

	dcosVipHost, exist := os.LookupEnv("FRAMEWORK_VIP_HOST")
	if !exist {
		errorl.Log("error", "environment variable FRAMEWORK_VIP_HOST cannot be empty")
		os.Exit(1)
	}

	lokiPort, exist := os.LookupEnv("LOKI_PORT")
	if !exist {
		lokiPort = "3100"
	}

	bufferDuration, exist := os.LookupEnv("LOG_BUFFER_DURATION")
	if !exist {
		bufferDuration = "5s"
	}

	d, _ := time.ParseDuration(bufferDuration)
	internalBuffer = &d

	lokiURL = fmt.Sprintf("http://loki.%s:%s/api/prom/push", dcosVipHost, lokiPort)

	info.Log("DC/OS Logging API", dcosLogAPI)
	info.Log("DC/OS Logging Prefix", loggingPrefix)
	info.Log("Loki URL", lokiURL)

	events := make(chan *sse.Event)
	dcosLogClient := sse.NewClient(dcosLogAPI)
	err := dcosLogClient.SubscribeChan("messages", events)
	if err != nil {
		errorl.Log("message", "sse channel subscribe failed.", "level", "error")
		os.Exit(1)
	}

	ch := make(chan t.DcosLog, 1000)
	go handleLogMessage(ch)
	defer close(ch)

	for {
		select {
		case e := <-events:
			dat := t.DcosLog{}
			if err := json.Unmarshal(e.Data, &dat); err != nil {
				errorl.Log("log is unparserable", err)
				err = dcosLogClient.SubscribeChan("messages", events)
				if err != nil {
					errorl.Log("message", "sse channel subscribe failed.", "level", "error")
					os.Exit(1)
				}
				info.Log("log channel re-subscribe success.", err)
				continue
			}

			if dat.Fields.DCOSSPACE != "" && strings.HasPrefix(dat.Fields.DCOSSPACE, loggingPrefix) && !strings.Contains(dat.Fields.CONTAINERID, ".check-") {
				ch <- dat
			}
		}
	}
}

func handleLogMessage(ch chan t.DcosLog) {
	ticker := time.NewTicker(*internalBuffer)
	defer ticker.Stop()
	for {
		select {
		case data := <-ch:
			//debug.Log("ts", obj.Timestamp, "instance", obj.Host.Name, "ns", obj.Kubernetes.Namespace, "pod", obj.Kubernetes.Pod.Name, "msg", obj.JSON.Log)
			collection = append(collection, &data)
		case <-ticker.C:
			if len(collection) == 0 {
				continue
			}
			debug.Log("msg", "sorting and processing", "len", len(collection))
			debug.Log("msg", "going to process messages", "len", len(collection))
			go sendToLoki(collection)
			collection = make(logCollection, 0, 1000)
		}
	}
}

func sendToLoki(col logCollection) {
	debug.Log("msg", "sendToLoki", "len", len(col))

	body := map[string][]l.Entry{}

	for _, dcosLog := range col {
		labels := fmt.Sprintf("{agent=\"%s\", container_id=\"%s\", dcos_space=\"%s\", executor_id=\"%s\", framework_id=\"%s\", stream=\"%s\", syslog_identifier=\"%s\"}",
			dcosLog.Fields.AGENTID,
			dcosLog.Fields.CONTAINERID,
			dcosLog.Fields.DCOSSPACE,
			dcosLog.Fields.EXECUTORID,
			dcosLog.Fields.FRAMEWORKID,
			dcosLog.Fields.STREAM,
			dcosLog.Fields.SYSLOGIDENTIFIER)
		entry := l.Entry{Timestamp: time.Unix(0, dcosLog.RealtimeTimestamp*1000), Line: dcosLog.Fields.MESSAGE}
		if val, ok := body[labels]; ok {
			body[labels] = append(val, entry)
		} else {
			body[labels] = []l.Entry{entry}
		}
	}

	streams := make([]*l.Stream, 0, len(body))

	for key, val := range body {
		stream := &l.Stream{
			Labels:  key,
			Entries: val,
		}
		sort.SliceStable(stream.Entries, func(i, j int) bool {
			return stream.Entries[i].Timestamp.Before(stream.Entries[j].Timestamp)
		})
		streams = append(streams, stream)
	}

	debug.Log("msg", fmt.Sprintf("sending %d streams to the server", len(streams)))
	req := l.PushRequest{
		Streams: streams,
	}

	buf, err := proto.Marshal(&req)

	if err != nil {
		errorl.Log("msg", "unable to marshall PushRequest", "err", err)
		return
	}

	buf = snappy.Encode(nil, buf)

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	httpreq, err := http.NewRequest(http.MethodPost, lokiURL, bytes.NewReader(buf))

	if err != nil {
		errorl.Log("msg", "unable to create http request", "err", err)
		return
	}

	httpreq = httpreq.WithContext(ctx)
	httpreq.Header.Add("Content-Type", "application/x-protobuf")

	client := http.Client{}

	resp, err := client.Do(httpreq)

	if err != nil {
		errorl.Log("msg", "http request error", "err", err)
		return
	}

	if resp.StatusCode/100 != 2 {
		scanner := bufio.NewScanner(io.LimitReader(resp.Body, 1024))
		line := ""
		if scanner.Scan() {
			line = scanner.Text()
		}
		err = fmt.Errorf("server returned HTTP status %s (%d): %s", resp.Status, resp.StatusCode, line)
		errorl.Log("mng", "server returned an error", "err", err)
		return
	}
	info.Log("msg", "sent streams to loki")
}
