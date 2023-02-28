package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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

	"github.com/jedib0t/go-pretty/v6/table"
)

func main() {
	var kubeconfigPath *string
	// var err error

	if home := homedir.HomeDir(); home != "" {
		kubeconfigPath = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfigPath = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	showHelp := flag.Bool("h", false, "Shows help")
	namespace := flag.String("n", "", "Namespace (defaults to current context)")
	flag.Parse()

	if *showHelp {
		flag.PrintDefaults()
		os.Exit(1)
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

	for {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("> ")
		query, err := reader.ReadString(';')
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			log.Fatal(err)
		}
		result, err := queryGabi(gabiUrl, query, bearerToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		} else if result.Error != "" {
			fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
		} else {
			//fmt.Println(result)
			formatResult(result, os.Stdout)
		}
	}

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

func convertToRow(raw []string) (r table.Row) {
	r = make(table.Row, len(raw))
	for i, cell := range raw {
		r[i] = interface{}(cell)
	}
	return
}
