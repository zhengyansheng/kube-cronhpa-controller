package controller

import (
	"encoding/json"
	"html/template"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"k8s.io/klog/v2"
)

type WebServer struct {
	cronManager *CronManager
}

func (ws *WebServer) Serve() {
	r := mux.NewRouter()
	r.HandleFunc("/api.json", ws.handleJobsController)
	r.HandleFunc("/index.html", ws.handleIndexController)
	r.HandleFunc("/", ws.handleIndexController)
	http.Handle("/", r)

	srv := &http.Server{
		Handler: r,
		Addr:    ":8000",
		// Good practice: enforce timeouts for servers you create!
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	klog.Fatal(srv.ListenAndServe())
}

type data struct {
	Items []Item
}

type Item struct {
	Id        string
	Name      string
	CronHPA   string
	Namespace string
	Pre       string
	Next      string
}

func (ws *WebServer) handleIndexController(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	entries := ws.cronManager.cronExecutor.ListEntries()
	klog.Infof("entries: %v", entries)
	klog.Infof("entries length: %v", len(entries))
	for i, entry := range entries {
		klog.Infof("entry %d: %v", i, entry)
	}
	d := data{Items: make([]Item, 0)}
	for _, e := range entries {
		job, ok := e.Job.(*CronJobHPA)
		//job, ok := e.Job.(CronJob)
		if !ok {
			klog.Warningf("Failed to parse cronjob %v to web console", e)
			continue
		}
		d.Items = append(d.Items, Item{
			Id:        job.ID(),
			Name:      job.Name(),
			CronHPA:   job.CronHPAMeta().Name,
			Namespace: job.CronHPAMeta().Namespace,
			Pre:       e.Prev.String(),
			Next:      e.Next.String(),
		})
	}

	tmpl, err := template.New("index").Parse(Template)
	if err != nil {
		klog.Errorf("Failed to parse template: %v", err)
	}
	tmpl.Execute(w, d)
}

func (ws *WebServer) handleJobsController(w http.ResponseWriter, r *http.Request) {
	b, err := json.Marshal(ws.cronManager.cronExecutor.ListEntries())
	if err != nil {
		w.Write([]byte(err.Error()))
		return
	}
	w.Write(b)
}

func NewWebServer(c *CronManager) *WebServer {
	return &WebServer{
		cronManager: c,
	}
}

const Template = `
<!doctype html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport"
          content="width=device-width, user-scalable=no, initial-scale=1.0, maximum-scale=1.0, minimum-scale=1.0">
    <meta http-equiv="X-UA-Compatible" content="ie=edge">
    <title>CronHPA Job Monitor</title>
</head>
<body>
	<center style="padding: 24px 0 24px 0">Cron Engine Job Monitor</center>
	<table class="gridtable">
      <tr>
		<th>CronHPA</th>
		<th>Namespace</th>	
		<th>Id</th>
		<th>Job</th>
		<th>Pre</th>
		<th>Next</th> 
      </tr>
{{range .Items}}
	  <tr>
	    <td>{{ .CronHPA }}</td>
		<td>{{ .Namespace }}</td>
		<td>{{ .Id }}</td>
		<td>{{ .Name }}</td>
		<td>{{ .Pre }}</td>
		<td>{{ .Next }}</td>
      </tr>
{{end}}
 	</table>
<style>
table.gridtable {margin:0 auto; font-family: verdana,arial,sans-serif;font-size:12px;color:#333333;border-width: 1px;border-color: #666666;border-collapse: collapse;}
table.gridtable th {border-width: 1px;padding: 8px;border-style: solid;border-color: #666666;background-color: #dedede;}
table.gridtable td {border-width: 1px;padding: 8px;border-style: solid;border-color: #666666;background-color: #ffffff;}
</style>
</body>
`
