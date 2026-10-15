package stylebundle

// Source is where an element most likely comes from in the page's code.
// Confidence runs from 0.1 (only hints to search for) to 0.95 (a dev build
// recorded the element's own file, line and column).
type Source struct {
	Framework  string   `json:"framework"`
	Component  string   `json:"component"`
	Components []string `json:"components"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Column     int      `json:"column"`
	Confidence float64  `json:"confidence"`
	Via        string   `json:"via"`
	Hints      []Hint   `json:"hints"`
}

// Hint is something to search the code for: a class, an id, a test id, the
// element's text (cut to 80 characters), its tag or a CSS-module class.
type Hint struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// ReadSource maps the first element matching selector to its source.
func ReadSource(p Page, selector string) (*Source, error) {
	var s Source
	if err := evalOn(p, "source", sourceJS, selector, map[string]int{"maxText": 80}, &s); err != nil {
		return nil, err
	}
	s.File = cleanURL(s.File)
	if len(s.Components) == 0 && s.Component != "" {
		s.Components = []string{s.Component}
	}
	if s.Components == nil {
		s.Components = []string{}
	}
	if s.Hints == nil {
		s.Hints = []Hint{}
	}
	return &s, nil
}
