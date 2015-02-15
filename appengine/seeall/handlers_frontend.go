// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

package seeall

import (
	"crypto/rand"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"appengine"
	"appengine/datastore"
	"appengine/user"
	"github.com/gorilla/mux"
	"github.com/mjibson/goon"
)

func init() {
	r := mux.NewRouter()
	frontendRoute(r)
	apiRoute(r)
	http.Handle("/", r)
}

func frontendRoute(r *mux.Router) {
	r.HandleFunc("/restricted/sources", GET(sourcesHdlr))
	r.HandleFunc("/restricted/sources/add", POST(sourcesAddHdlr))
	r.HandleFunc("/restricted/source/{id:[0-9]+}", GET(sourceHdlr))
	r.HandleFunc("/restricted/source/{id:[0-9]+}/delete", POST(sourceDeleteHdlr))
}

var sourcesTmpl = template.Must(template.New("sources").Parse(`
<html>
  <head>
    <title>See All Sources</title>
		<style>
		</style>
  </head>
  <body>
		<h1>Sources</h1>
		<ul>
		{{range $index, $source := .Sources}}
			<li>
				{{$source.Who}} - {{$source.Created}} - <a href="/restricted/source/{{$source.ID}}">"{{$source.Name}}"</a> - "{{$source.Details}}" - "{{$source.SecretBase64}}" - {{$source.WhitelistIP}}
				<form action="/restricted/source/{{with index $.SourceKeys $index}}{{.IntID}}{{end}}/delete" method="POST">
					<input type="submit" value="Delete">
				</form>
			</li>
    {{end}}
		</ul>
		<form action="/restricted/sources/add" method="POST">
			Name: <input type="text" name="Name"></input><br>
			Description: <input type="text" name="Details"></input><br>
			WhitelistIP: <input type="text" name="WhitelistIP" value="0.0.0.0/0"></input><br>
			<input type="submit" value="Add">
		</form>
  </body>
</html>
`))

func GET(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Only GET is supported", http.StatusMethodNotAllowed)
			return
		}
		f(w, r)
	}
}

func POST(f http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Only POST is supported", http.StatusMethodNotAllowed)
			return
		}
		f(w, r)
	}
}

func sourcesHdlr(w http.ResponseWriter, r *http.Request) {
	n := goon.NewGoon(r)
	q := datastore.NewQuery("Source").Order("__key__")
	data := struct {
		SourceKeys []*datastore.Key
		Sources    []Source
	}{}
	keys, err := n.GetAll(q, &data.Sources)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.SourceKeys = keys
	if err := sourcesTmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func sourcesAddHdlr(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	u := user.Current(c)
	n := goon.NewGoon(r)
	// TODO(maruel): XSRF token.
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	keys, err := datastore.NewQuery("Source").KeysOnly().Order("__key__").GetAll(c, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dummy := &Source{}
	if len(keys) != 0 {
		dummy.ID = keys[len(keys)-1].IntID()
	}
	source := &Source{
		Who:         u.String(),
		Created:     time.Now().UTC(),
		RemoteAddr:  r.RemoteAddr,
		Name:        r.FormValue("Name"),
		Details:     r.FormValue("Details"),
		Secret:      random,
		WhitelistIP: r.FormValue("WhitelistIP"),
	}
	is := &ImageStream{ID: 1}
	entities := []interface{}{source, is}

	opts := &datastore.TransactionOptions{}
	for {
		if err := n.RunInTransaction(func(tg *goon.Goon) error {
			dummy.ID++
			if err := tg.Get(dummy); err != datastore.ErrNoSuchEntity {
				// Force to continue to loop.
				return datastore.ErrNoSuchEntity
			}
			source.ID = dummy.ID
			is.Parent = tg.Key(source)
			if _, err := n.PutMulti(entities); err != nil {
				return err
			}
			return nil
		}, opts); err == nil {
			break
		}
	}
	http.Redirect(w, r, "/restricted/sources", http.StatusFound)
}

var sourceTmpl = template.Must(template.New("source").Parse(`
<html>
  <head>
    <title>See All Source {{.Source.Name}}</title>
  </head>
  <body>
		<h1>Source {{.Source.Name}}</h1>
		{{.Source}}<br>
		{{.ImageStream}}
		<ul>
		{{range .Images}}
			<img src="data:image/png;base64,{{.PNGBase64}}"></img><br>
    {{end}}
  </body>
</html>
`))

func sourceHdlr(w http.ResponseWriter, r *http.Request) {
	n := goon.NewGoon(r)
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := struct {
		Source      Source
		ImageStream ImageStream
		Images      []Image
	}{
		Source:      Source{ID: int64(id)},
		ImageStream: ImageStream{ID: 1},
	}
	data.ImageStream.Parent = n.Key(data.Source)
	entities := []interface{}{&data.Source, &data.ImageStream}
	if err := n.GetMulti(entities); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	items := data.ImageStream.NextID
	if data.ImageStream.NextID > 16 {
		items = 16
	}
	data.Images = make([]Image, items)
	isKey := n.Key(data.ImageStream)
	for i := range data.Images {
		data.Images[i].ID = data.ImageStream.NextID - int64(i) - 1
		data.Images[i].Parent = isKey
	}
	if err := n.GetMulti(data.Images); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := sourceTmpl.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return
}

func sourceDeleteHdlr(w http.ResponseWriter, r *http.Request) {
	// TODO(maruel): XSRF token.
	id, err := strconv.Atoi(mux.Vars(r)["id"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	n := goon.NewGoon(r)
	// Doesn't delete ImageStream.
	if err := n.Delete(n.Key(&Source{ID: int64(id)})); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/restricted/sources", http.StatusFound)
	return
}
