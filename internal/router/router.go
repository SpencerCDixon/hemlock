package router

import (
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/gschier/hemlock"
	"github.com/gschier/hemlock/interfaces"
	"github.com/gschier/hemlock/internal/templates"
	"net/http"
	"path/filepath"
)

type Router struct {
	app         *hemlock.Application
	root        chi.Router
	middlewares []interfaces.Middleware
}

func NewRouter(app *hemlock.Application) *Router {
	root := chi.NewRouter()

	root.Use(middleware.Recoverer)
	root.Use(middleware.DefaultCompress)
	root.Use(middleware.CloseNotify)
	root.Use(middleware.RedirectSlashes)

	if app.Config.Env == "development" {
		root.Use(middleware.Logger)
	}

	if app.Config.Env == "production" {
		root.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ext := filepath.Ext(r.URL.Path)
				if ext == ".css" || ext == ".js" {
					w.Header().Add("Cache-Control", "public, max-age=7200")
				}
				next.ServeHTTP(w, r)
			})
		})
		root.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Forwarded-Proto") == "http" {
					newUrl := "https://" + r.Host + r.URL.String()
					http.Redirect(w, r, newUrl, http.StatusFound)
				} else {
					next.ServeHTTP(w, r)
				}
			})
		})
	}

	router := &Router{root: root, app: app}
	router.root.NotFound(router.serve(func(req interfaces.Request, res interfaces.Response) interfaces.Result {
		return res.Data("Not Found").Status(404).End()
	}))
	return router
}

func (router *Router) Redirect(uri, to string, code int) {
	router.root.HandleFunc(uri, func(res http.ResponseWriter, req *http.Request) {
		http.Redirect(res, req, to, code)
	})
}

func (router *Router) Get(uri string, callback interfaces.Callback) {
	router.addRoute([]string{http.MethodGet}, uri, callback)
}

func (router *Router) Post(uri string, callback interfaces.Callback) {
	router.addRoute([]string{http.MethodPost}, uri, callback)
}

func (router *Router) Put(uri string, callback interfaces.Callback) {
	router.addRoute([]string{http.MethodPut}, uri, callback)
}

func (router *Router) Patch(uri string, callback interfaces.Callback) {
	router.addRoute([]string{http.MethodPatch}, uri, callback)
}

func (router *Router) Delete(uri string, callback interfaces.Callback) {
	router.addRoute([]string{http.MethodDelete}, uri, callback)
}

func (router *Router) Options(uri string, callback interfaces.Callback) {
	router.addRoute([]string{http.MethodOptions}, uri, callback)
}

func (router *Router) Any(uri string, callback interfaces.Callback) {
	router.addRoute(nil, uri, callback)
}

func (router *Router) Match(methods []string, uri string, callback interfaces.Callback) {
	router.addRoute(methods, uri, callback)
}

func (router *Router) Use(m ...interfaces.Middleware) {
	router.middlewares = append(router.middlewares, m...)
}

func (router *Router) With(m ...interfaces.Middleware) interfaces.Router {
	newRouter := &Router{
		root: router.root.With(),
		app:  router.app,
	}
	return newRouter
}

// Handler returns the HTTP handler
func (router *Router) Handler() http.Handler {
	return router.root
}
func (router *Router) callNext(i int, req interfaces.Request, res interfaces.Response) interfaces.Result {
	if i == len(router.middlewares) {
		return nil
	}

	fn := router.middlewares[i]
	view := fn(req, res, func(newReq interfaces.Request, newRes interfaces.Response) interfaces.Result {
		return router.callNext(i+1, newReq, newRes)
	})

	return view
}

func (router *Router) addRoute(methods []string, pattern string, callback interface{}) {
	if len(methods) == 0 {
		router.root.HandleFunc(pattern, router.serve(callback))
	}

	for _, m := range methods {
		router.root.MethodFunc(m, pattern, router.serve(callback))
	}
}

func (router *Router) serve(callback interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var renderer templates.Renderer
		router.app.Resolve(&renderer)

		req := newRequest(r)
		res := newResponse(w, r, &renderer)

		result := router.callNext(0, req, res)

		// The middleware sent a response so we're done
		if result != nil {
			return
		}

		newApp := hemlock.CloneApplication(router.app)
		newApp.Instance(req)
		newApp.Instance(res)

		c := chi.RouteContext(r.Context())
		extraArgs := make([]interface{}, len(c.URLParams.Values))
		for i, v := range c.URLParams.Values {
			extraArgs[i] = v
		}

		results := newApp.ResolveInto(callback, extraArgs...)
		if len(results) != 1 {
			panic("Route did not return a value. Got " + string(len(results)))
		}

		var ok bool
		result, ok = results[0].(interfaces.Result)
		if !ok {
			panic("Route did not return View instance")
		}
	}
}
