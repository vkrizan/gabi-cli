package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/app-sre/gabi/pkg/models"
	routev1 "github.com/openshift/api/route/v1"
	routeclientv1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	"github.com/c-bata/go-prompt"
	"github.com/jedib0t/go-pretty/v6/table"
)

var expandedDisplay bool

func main() {
	var kubeconfigPath *string

	if home := homedir.HomeDir(); home != "" {
		kubeconfigPath = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfigPath = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	showHelp := flag.Bool("h", false, "Shows help")
	quiet := flag.Bool("q", false, "Suppress logging messages")
	namespace := flag.String("n", "", "Namespace (defaults to current context)")
	flag.Parse()

	if *showHelp {
		flag.PrintDefaults()
		os.Exit(1)
	}

	if *quiet {
		log.SetOutput(ioutil.Discard)
	}
	kubeconfig, config := setupK8s(*kubeconfigPath)
	setDefaultNamespace(kubeconfig, namespace)

	bearerToken := config.BearerToken
	if bearerToken == "" {
		log.Fatalf("no Bearer Token please use `oc login`")
	}

	log.Printf("Looking up Gabi from namespace %s, cluster %s", *namespace, config.Host)
	gabiRoute, err := getGabiRoute(config, *namespace)

	if err != nil {
		if apierrors.IsUnauthorized(err) {
			log.Fatalf("%s, please login with oc login", err)
		} else {
			log.Fatalf("couldn't find Gabi instance: %s", err)
		}
	}

	gabiUrl := gabiUrlFromRoute(gabiRoute)
	log.Printf("Using Gabi %s", gabiUrl)

	var query string
	if len(flag.Args()) > 0 {
		// if there's a query on commandline, just run it
		query = strings.Join(flag.Args(), " ")
		runQuery(gabiUrl, bearerToken, "", &query)
		return
	}
	p := prompt.New(func(input string) {
		runQuery(gabiUrl, bearerToken, input, &query)
	}, completer)
	p.Run()
}

func runQuery(gabiUrl, bearerToken, input string, query *string) {
	*query = fmt.Sprintf("%s%s", *query, input)

	// Meta-commands start with \ and don't require a semicolon.
	trimmed := strings.TrimSpace(*query)
	if strings.HasPrefix(trimmed, "\\") {
		*query = ""
		handleMetaCommand(trimmed)
		return
	}

	if !strings.HasSuffix(*query, ";") {
		*query = fmt.Sprintf("%s\n", *query)
		return
	}
	*query = strings.TrimSpace(*query)
	result, err := queryGabi(gabiUrl, *query, bearerToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
	} else if result.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
	} else {
		formatResult(result, os.Stdout)
	}
	*query = ""
}

// handleMetaCommand processes backslash commands entered by the user.
// Supported commands:
//
//	\x [on|off|toggle]  — toggle expanded (vertical) display mode
func handleMetaCommand(cmd string) {
	parts := strings.Fields(cmd)
	switch parts[0] {
	case "\\x":
		arg := "toggle"
		if len(parts) > 1 {
			arg = parts[1]
		}
		switch arg {
		case "on":
			expandedDisplay = true
		case "off":
			expandedDisplay = false
		case "toggle":
			expandedDisplay = !expandedDisplay
		default:
			fmt.Fprintf(os.Stderr, "\\x: unknown argument %q\n", arg)
			return
		}
		state := "off"
		if expandedDisplay {
			state = "on"
		}
		fmt.Printf("Expanded display is %s.\n", state)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", parts[0])
	}
}

func completer(in prompt.Document) []prompt.Suggest {
	return []prompt.Suggest{}
}

func setupK8s(kubeconfigPath string) (clientcmd.ClientConfig, *restclient.Config) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.ExplicitPath = kubeconfigPath
	kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})

	// use the current context in kubeconfig
	clientconfig, err := kubeconfig.ClientConfig()
	if err != nil {
		log.Fatal(err.Error())
	}
	return kubeconfig, clientconfig
}

func setDefaultNamespace(kubeconfig clientcmd.ClientConfig, namespace *string) {
	if *namespace == "" {
		var err error
		*namespace, _, err = kubeconfig.Namespace()
		if err != nil {
			log.Fatal(err.Error())
		}
	}
}

func getGabiRoute(config *restclient.Config, namespace string) (gabi routev1.Route, err error) {
	clientset, err := routeclientv1.NewForConfig(config)
	if err != nil {
		return
	}
	routes, err := clientset.Routes(namespace).List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		return
	}

	for _, route := range routes.Items {
		if strings.HasPrefix(route.Name, "gabi-") {
			gabi = route
			return
		}
	}
	err = fmt.Errorf("no gabi route found in namespace %s", namespace)
	return
}

func gabiUrlFromRoute(route routev1.Route) string {
	var proto = "https"
	if route.Spec.TLS == nil {
		proto = "https"
	}
	return fmt.Sprintf("%s://%s%s", proto, route.Spec.Host, route.Spec.Path)
}

func queryGabi(url, query, token string) (models.QueryResponse, error) {
	reqModel := models.QueryRequest{Query: query}
	reqData, err := json.Marshal(reqModel)
	if err != nil {
		return models.QueryResponse{}, fmt.Errorf("marshal of query failed: %w", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/query", url), bytes.NewReader(reqData))
	if err != nil {
		return models.QueryResponse{}, fmt.Errorf("request build failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.QueryResponse{}, fmt.Errorf("gabi request failed: %w", err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		return models.QueryResponse{}, fmt.Errorf("http status: %s", resp.Status)
	}

	dec := json.NewDecoder(resp.Body)
	result := models.QueryResponse{}
	if e := dec.Decode(&result); e != nil {
		err = fmt.Errorf("malformed result %w", e)
	}
	return result, err
}

func formatResult(r models.QueryResponse, out io.Writer) {
	if expandedDisplay {
		formatResultExpanded(r, out)
		return
	}
	t := table.NewWriter()
	t.SetOutputMirror(out)
	if len(r.Result) > 0 {
		t.AppendHeader(convertToRow(r.Result[0]))
	}
	if len(r.Result) > 0 {
		for _, row := range r.Result[1:] {
			t.AppendRow(convertToRow(row))
		}
	}
	t.Style().Options.DrawBorder = false
	t.Render()
}

// formatResultExpanded renders each row as a vertical key/value list,
// matching the output style of psql's \x on mode.
func formatResultExpanded(r models.QueryResponse, out io.Writer) {
	if len(r.Result) < 1 {
		return
	}
	headers := r.Result[0]

	maxColLen := 0
	for _, h := range headers {
		if len(h) > maxColLen {
			maxColLen = len(h)
		}
	}

	for i, row := range r.Result[1:] {
		// Separator: "-[ RECORD N ]" padded with dashes to at least cover the column width.
		sep := fmt.Sprintf("-[ RECORD %d ]", i+1)
		if pad := maxColLen + 2 - len(sep); pad > 0 {
			sep += strings.Repeat("-", pad)
		}
		fmt.Fprintln(out, sep)

		for j, val := range row {
			col := ""
			if j < len(headers) {
				col = headers[j]
			}
			fmt.Fprintf(out, "%-*s | %s\n", maxColLen, col, val)
		}
	}
}

func convertToRow(raw []string) (r table.Row) {
	r = make(table.Row, len(raw))
	for i, cell := range raw {
		r[i] = interface{}(cell)
	}
	return
}
