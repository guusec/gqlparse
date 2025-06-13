package main

import (
        "encoding/json"
        "flag"
        "fmt"
        "io/ioutil"
        "log"
        "os"
        "strings"
)

// Banner to be shown in the help prompt.
const banner = `
█████▀███████████████████████████████████████████
█─▄▄▄▄█─▄▄▄─█▄─▄███▄─▄▄─██▀▄─██▄─▄▄▀█─▄▄▄▄█▄─▄▄─█
█─██▄─█─██▀─██─██▀██─▄▄▄██─▀─███─▄─▄█▄▄▄▄─██─▄█▀█
▀▄▄▄▄▄▀───▄▄▀▄▄▄▄▄▀▄▄▄▀▀▀▄▄▀▄▄▀▄▄▀▄▄▀▄▄▄▄▄▀▄▄▄▄▄▀
`

// IntrospectionResponse represents the root of an introspection query response.
type IntrospectionResponse struct {
        Data struct {
                Schema Schema `json:"__schema"`
        } `json:"data"`
}

// Schema represents the GraphQL schema.
type Schema struct {
        QueryType        NamedTypeRef  `json:"queryType"`
        MutationType     *NamedTypeRef `json:"mutationType"`
        SubscriptionType *NamedTypeRef `json:"subscriptionType"`
        Types            []FullType    `json:"types"`
}

// NamedTypeRef represents a type reference with just a name.
type NamedTypeRef struct {
        Name string `json:"name"`
}

// FullType represents a type definition from the introspection result.
type FullType struct {
        Kind          string         `json:"kind"`
        Name          string         `json:"name"`
        Fields        []Field        `json:"fields"`
        InputFields   []InputValue   `json:"inputFields"`
        EnumValues    []EnumValue    `json:"enumValues"`
        PossibleTypes []NamedTypeRef `json:"possibleTypes"`
}

// Field represents a field (or operation argument in mutation and query types).
type Field struct {
        Name        string       `json:"name"`
        Description string       `json:"description"`
        Args        []InputValue `json:"args"`
        Type        TypeRef      `json:"type"`
}

// InputValue represents an argument or input field.
type InputValue struct {
        Name         string  `json:"name"`
        Description  string  `json:"description"`
        Type         TypeRef `json:"type"`
        DefaultValue *string `json:"defaultValue"`
}

// EnumValue represents an enum value definition.
type EnumValue struct {
        Name        string `json:"name"`
        Description string `json:"description"`
}

// TypeRef represents a type reference that may be wrapped (e.g., NON_NULL, LIST).
type TypeRef struct {
        Kind   string   `json:"kind"`
        Name   *string  `json:"name"`
        OfType *TypeRef `json:"ofType"`
}

// getTypeString returns a string representing the type reference for GraphQL variable declarations.
func getTypeString(t TypeRef) string {
        switch t.Kind {
        case "NON_NULL":
                // Unwrap and add an exclamation mark.
                return getTypeString(*t.OfType) + "!"
        case "LIST":
                return "[" + getTypeString(*t.OfType) + "]"
        default:
                if t.Name != nil {
                        return *t.Name
                }
                return ""
        }
}

// unwrap returns the innermost type, stripping NON_NULL and LIST wrappers.
func unwrap(t TypeRef) TypeRef {
        if t.Kind == "NON_NULL" || t.Kind == "LIST" {
                return unwrap(*t.OfType)
        }
        return t
}

// isComposite checks whether the type is an object, interface, or union.
func isComposite(t TypeRef) bool {
        inner := unwrap(t)
        return inner.Kind == "OBJECT" || inner.Kind == "INTERFACE" || inner.Kind == "UNION"
}

// generateOperation builds a GraphQL operation (query or mutation) string for a given field.
func generateOperation(f Field, opType string) string {
        var varDefs []string
        var argAssignments []string

        // Generate variable definitions for every argument.
        for _, arg := range f.Args {
                varDef := fmt.Sprintf("$%s: %s", arg.Name, getTypeString(arg.Type))
                varDefs = append(varDefs, varDef)
                // Use the variable in the field call.
                argAssignment := fmt.Sprintf("%s: $%s", arg.Name, arg.Name)
                argAssignments = append(argAssignments, argAssignment)
        }

        // Build the operation header.
        var header string
        if len(varDefs) > 0 {
                header = fmt.Sprintf("%s %s(%s)", opType, f.Name, strings.Join(varDefs, ", "))
        } else {
                header = opType
        }

        // Build the field call with arguments if present.
        call := f.Name
        if len(argAssignments) > 0 {
                call += "(" + strings.Join(argAssignments, ", ") + ")"
        }

        // If the operation returns a composite type, add a simple selection set.
        var selection string
        if isComposite(f.Type) {
                selection = " { __typename }"
        }

        // Assemble and return the full operation.
        operation := fmt.Sprintf("%s { %s%s }", header, call, selection)
        return operation
}

// findTypeByName searches for a full type by name in the schema’s types.
func findTypeByName(types []FullType, name string) *FullType {
        for _, t := range types {
                if t.Name == name && t.Fields != nil {
                        return &t
                }
        }
        return nil
}

func main() {
        // Override the default usage function to print the banner.
        flag.Usage = func() {
                // Print the banner.
                fmt.Fprintf(os.Stderr, "%s\n", banner)
                // Print the ordinary usage.
                fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
                flag.PrintDefaults()
        }

        // Use -i for specifying the introspection JSON file.
        schemaFile := flag.String("i", "", "JSON file with the GraphQL introspection schema")
        // The -m flag indicates that mutations should also be generated.
        includeMutations := flag.Bool("m", false, "Include mutations in generation")
        flag.Parse()

        if *schemaFile == "" {
                flag.Usage()
                os.Exit(1)
        }

        // Read the schema file.
        data, err := ioutil.ReadFile(*schemaFile)
        if err != nil {
                log.Fatalf("Error reading file: %v", err)
        }

        // Unmarshal the JSON.
        var introspection IntrospectionResponse
        err = json.Unmarshal(data, &introspection)
        if err != nil {
                log.Fatalf("Error parsing JSON: %v", err)
        }

        schema := introspection.Data.Schema

        // Process Query operations.
        queryTypeName := schema.QueryType.Name
        queryType := findTypeByName(schema.Types, queryTypeName)
        if queryType == nil {
                log.Fatalf("Could not find query type with name %s", queryTypeName)
        }

        // Generate queries for each field in the Query type.
        for _, f := range queryType.Fields {
                op := generateOperation(f, "query")
                // Print each generated operation on one line followed by an extra empty line.
                fmt.Println(op)
                fmt.Println()
        }

        // If the -m flag is provided, do the same for mutations.
        if *includeMutations {
                if schema.MutationType == nil {
                        log.Println("No mutations defined in the schema.")
                } else {
                        mutationTypeName := schema.MutationType.Name
                        mutationType := findTypeByName(schema.Types, mutationTypeName)
                        if mutationType == nil {
                                log.Fatalf("Could not find mutation type with name %s", mutationTypeName)
                        }
                        for _, f := range mutationType.Fields {
                                op := generateOperation(f, "mutation")
                                fmt.Println(op)
                                fmt.Println()
                        }
                }
        }
}
