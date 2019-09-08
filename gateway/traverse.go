package gateway

import (
	"net/url"
	"strings"

	gabs "github.com/Jeffail/gabs/v2"
	log "github.com/sirupsen/logrus"
)

func createPointer(parts []string) string {
	return "/" + strings.Join(parts, "/")
}

func unescape(s string) string {
	return strings.ReplaceAll(s, "~2", "*")
}

// traverseJSON recursively traverses the JSON document in order to filter it, rewrite relations URLs, and pass the relations to a closure
// TODO: a better implementation could be to convert both fields and preload selectors in a single tree, then traverse it, in a single pass.
//       It would improve performance and allow to preserve the original order of keys
func (g *Gateway) traverseJSON(key string, pointers []string, currentRawJSON interface{}, newRawJSON interface{}, relationHandler func(*url.URL)) interface{} {
	currentJSON := gabs.Wrap(currentRawJSON)

	var newJSON *gabs.Container
	if newRawJSON == nil {
		newJSON = gabs.New()
	} else {
		newJSON = gabs.Wrap(newRawJSON)
	}

	// TODO: preserve JSON objects order
	for _, propertyPointer := range pointers {
		parts := strings.Split(strings.Trim(propertyPointer, "/"), "/")
		l := len(parts)
		subJSON := currentJSON

		for i, path := range parts {
			if path == "*" {
				// Objects
				childrenObj := subJSON.ChildrenMap()
				if len(childrenObj) != 0 {
					log.WithFields(log.Fields{"pointer": createPointer(parts), "path": path}).Info("Looping over objects isn't supported yet")
					// Actually, I'm not sure if that's a good idea at all to support that...
					break
				}

				// Array
				childrenArr := subJSON.Children()
				if childrenArr == nil {
					log.WithFields(log.Fields{"pointer": createPointer(parts), "path": path}).Info("Structure isn't a collection")
					break
				}

				arrayPointer := unescape(createPointer(parts[:i]))
				currentArrayValues, err := newJSON.JSONPointer(arrayPointer)
				var newArray []interface{}

				for j, child := range childrenArr {
					var currentArrayValue interface{}
					if err == nil {
						if currentArrayElement, err := currentArrayValues.ArrayElement(j); err == nil {
							currentArrayValue = currentArrayElement.Data()
						}
					}

					newArrayValue := g.traverseJSON(key, []string{createPointer(parts[i+1:])}, child.Data(), currentArrayValue, relationHandler)
					newArray = append(newArray, newArrayValue)
				}
				newJSON.SetJSONPointer(newArray, arrayPointer)

				break
			}

			newSubJSON, err := subJSON.JSONPointer("/" + unescape(path))
			if err != nil {
				s, ok := subJSON.Data().(string)
				if !ok {
					log.WithFields(log.Fields{"pointer": createPointer(parts), "path": path}).Debug("Cannot resolve JSON Pointer")
					break
				}

				u, err := url.Parse(s)
				if err != nil {
					// Not an URL
					log.WithFields(log.Fields{"pointer": createPointer(parts), "path": path, "relation": u}).Debug("Cannot resolve JSON Pointer (invalid relation)")
					break
				}

				subPointer := createPointer(parts[i:])
				if subPointer != "/" {
					q := u.Query()
					q.Add(key, subPointer)
					u.RawQuery = q.Encode()
				}

				newURL := u.String()
				pointer := createPointer(parts[:i])

				if relationHandler != nil {
					relationHandler(u)
				}

				log.WithFields(log.Fields{"pointer": createPointer(parts), "path": path, "relation": s}).Debug("URL rewrote")

				if pointer == "/" {
					// Rewrite the root
					return newURL
				}
				newJSON.SetJSONPointer(newURL, unescape(pointer))

				break
			}

			if i == l-1 {
				// Found! Include this value in the new document.
				newJSON.SetJSONPointer(newSubJSON.Data(), unescape(createPointer(parts)))
				break
			}

			subJSON = newSubJSON
		}
	}

	return newJSON.Data()
}
