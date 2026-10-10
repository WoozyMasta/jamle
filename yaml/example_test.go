package yaml_test

import (
	"fmt"

	"github.com/woozymasta/jamle/yaml"
)

func ExampleUnmarshal() {
	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte("host: localhost\nport: 8080\n"), &cfg); err != nil {
		panic(err)
	}

	fmt.Printf("%s:%d\n", cfg.Host, cfg.Port)
	// Output: localhost:8080
}

func ExampleJSONToYAML() {
	out, err := yaml.JSONToYAML([]byte(`{"host":"localhost","port":8080}`))
	if err != nil {
		panic(err)
	}

	fmt.Print(string(out))
	// Output:
	// host: localhost
	// port: 8080
}

func ExampleYAMLToJSON() {
	out, err := yaml.YAMLToJSON([]byte("host: localhost\nport: 8080\n"))
	if err != nil {
		panic(err)
	}

	fmt.Print(string(out))
	// Output: {"host":"localhost","port":8080}
}

func ExampleMarshalWith() {
	type Config struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}

	out, err := yaml.MarshalWith(Config{Host: "localhost", Port: 8080}, yaml.WriteOptions{
		Format: yaml.FormatJSON,
		Indent: 2,
	})
	if err != nil {
		panic(err)
	}

	fmt.Print(string(out))
	// Output:
	// {
	//   "host": "localhost",
	//   "port": 8080
	// }
}
