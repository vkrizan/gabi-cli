# Gabi CLI

This is a CLI written for the GABI service. For details about GABI, [look here](https://github.com/app-sre/gabi).

To run this CLI you will need a current session into the Openshift cluster you would like to query. Please make sure you are logged into the cluster via `oc login` before running Gabi CLI.

You also need to be connected to the Openshift project running the database container that you wish to query. You can do so by running `oc project <projectname>`.

## Installation

You can download and install the latest release of the Gabi CLI with this command:  
`go install github.com/vkrizan/gabi-cli@latest`

After which you should be able to run it with `gabi-cli`.

If you receive a `command not found` error, make sure that your go bin directory has been added to your path. For information on what your go bin directory is, run `go help install`.

## Usage
```
Usage of gabi-cli:
  -h    Shows help
  -kubeconfig string
        (optional) absolute path to the kubeconfig file (default "~/.kube/config")
  -n string
        Namespace (defaults to current context)
  -q    Suppress logging messages
```

If your system is correctly configured (logged into Openshift and a GABI compliant project selected), then running `gabi-cli` should report the namespace, cluster, and GABI url you have accessed and drop you into a database query prompt.

In this prompt you may interact with the database via SQL query strings.

Some examples:  
`SELECT * FROM pg_catalog.pg_tables;` -> List all tables for a Postgres database  
`SELECT COUNT(*) FROM <table>;` -> Count the number of rows in table \<table\>  
`SELECT column_name, data_type FROM information_schema.columns WHERE table_name = '<table>';` -> List column names and types for \<table\> in a Postgres database.

**NOTE:** Only query strings may be used. PSQL commands, such as `\d` and `\gdesc` will not work.

You may scroll through the command history with the up and down arrow keys.

`Ctrl + L` will clear the screen.

`Ctrl + D` will quit the prompt and exit Gabi CLI.
