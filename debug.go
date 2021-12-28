package lrpc

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"text/template"
)

type debugHTTP struct {
	*Server
}

type debugService struct {
	Name   string
	Method map[string]*methodType
}

// debug page running at _/debug/lrpc
func (server debugHTTP) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	debugHtml, err := ioutil.ReadFile("../debug.html")
	if err != nil {
		log.Panic("[RPC Debug] cannot find debug.html: ", err)
		return
	}
	debugPage := template.Must(template.New("RPC debug").Parse(string(debugHtml)))

	var services []debugService
	server.serviceMap.Range(func(namei, svci interface{}) bool {
		svc := svci.(*service)
		services = append(services, debugService{
			Name:   namei.(string),
			Method: svc.method,
		})
		return true
	})
	err = debugPage.Execute(w, services)
	if err != nil {
		_, _ = fmt.Fprintln(w, "[RPC] error while executing template:", err.Error())
	}
}
