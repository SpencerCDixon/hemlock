package templates

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

type Renderer struct {
	root      string
	funcs     template.FuncMap
	templates map[string]map[string]*template.Template
}

func NewRenderer(root string, funcs template.FuncMap) *Renderer {
	return &Renderer{root: root, funcs: funcs}
}

func (r *Renderer) Init() error {
	templatePaths, err := r.findTemplates(r.root, "views")
	if err != nil {
		return err
	}

	layoutPaths, err := r.findTemplates(r.root, "layouts")
	if err != nil {
		return err
	}

	partialPaths, err := r.findTemplates(r.root, "partials")
	if err != nil {
		return err
	}

	// Create all possible combinations of templates to bases
	r.templates = map[string]map[string]*template.Template{}
	for _, templatePath := range templatePaths {
		templateName := filepath.Base(templatePath)
		r.templates[templateName] = map[string]*template.Template{}
		for _, layoutPath := range append(layoutPaths, "") {
			var paths []string
			if layoutPath == "" {
				paths = append(partialPaths, templatePath)
			} else {
				// NOTE: Layout must be parsed before template so {{ block }} defaults work
				paths = append(partialPaths, layoutPath, templatePath)
			}

			t, err := template.New("").Funcs(r.funcs).ParseFiles(paths...)

			if err != nil {
				return err
			}

			var layoutName string
			if layoutPath == "" {
				layoutName = ""
			} else {
				layoutName = filepath.Base(layoutPath)
			}

			r.templates[templateName][layoutName] = t
		}
	}

	fmt.Printf("[renderer] Parsed %d templates\n", len(templatePaths))

	return nil
}

func (r *Renderer) RenderString(w io.Writer, html string, data interface{}) error {
	t, err := template.New("").Funcs(r.funcs).Parse(html)
	if err != nil {
		return err
	}

	return t.Execute(w, data)
}

func (r *Renderer) RenderTemplate(w io.Writer, template, layout string, data interface{}) error {
	if len(r.templates) == 0 {
		return errors.New(fmt.Sprintf("No templates found in %s", r.root))
	}

	if _, ok := r.templates[template]; !ok {
		templates := make([]string, 0)
		for name := range r.templates {
			templates = append(templates, name)
		}
		options := strings.Join(templates, ", ")
		return errors.New(fmt.Sprintf("Template not found '%s'. Options are %s", template, options))
	}

	t, ok := r.templates[template][layout]
	if !ok {
		return errors.New(fmt.Sprintf("Layout (%s) not found. Options %#v", layout, r.templates[template]))
	}

	if layout == "" {
		return t.ExecuteTemplate(w, template, data)
	} else {
		return t.ExecuteTemplate(w, layout, data)
	}
}

func (r *Renderer) findTemplates(dirs ...string) ([]string, error) {
	dir := filepath.Join(dirs...)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return make([]string, 0), nil
	}

	fileInfo, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0)
	for _, f := range fileInfo {
		if f.IsDir() {
			// TODO: Implement recursive search
			continue
		}
		paths = append(paths, filepath.Join(dir, f.Name()))
	}

	return paths, nil
}
