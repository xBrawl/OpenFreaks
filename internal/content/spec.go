package content

type Spec struct {
	ID    int
	Value any
}

func (s Spec) Exists() bool       { return s.Value != nil }
func (s Spec) Text() string       { return asStr(s.Value, "") }
func (s Spec) At(key string) Spec { return Spec{Value: asMap(s.Value)[key]} }
func (s Spec) Int(key string, fallback ...int) int {
	d := 0
	if len(fallback) > 0 {
		d = fallback[0]
	}
	return asInt(s.At(key).Value, d)
}
func (s Spec) Number(fallback ...int) int {
	d := 0
	if len(fallback) > 0 {
		d = fallback[0]
	}
	return asInt(s.Value, d)
}
func (s Spec) String(key string, fallback ...string) string {
	d := ""
	if len(fallback) > 0 {
		d = fallback[0]
	}
	return asStr(s.At(key).Value, d)
}
func (s Spec) Bool(key string, fallback ...bool) bool {
	d := false
	if len(fallback) > 0 {
		d = fallback[0]
	}
	return asBool(s.At(key).Value, d)
}
func (s Spec) List() []Spec {
	v, _ := s.Value.([]any)
	result := make([]Spec, len(v))
	for i, x := range v {
		result[i] = Spec{Value: x}
	}
	return result
}

func (c *CatalogData) Spec(table, name string) Spec {
	for _, d := range c.Definitions[table] {
		if d.String("name") == name {
			return d
		}
	}
	return Spec{}
}
func (c *CatalogData) SpecByID(table string, id int) Spec {
	for _, d := range c.Definitions[table] {
		if d.ID == id {
			return d
		}
	}
	return Spec{}
}
