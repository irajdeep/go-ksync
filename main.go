package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	configmapName = flag.String("config-map-name", "namespace-controller", "pass the configmap name to be watched")
	syncInterval  = flag.Duration("sync-duration", 10*time.Second, "sync interval for syncing configmap")
	namespace     = flag.String("namespace", "namespace-controller", "namespace the syncer cares to sync the cm in")
)

// Constants to connect to local database
const (
	DbHost = "tcp(127.0.0.1:3306)"
	DbName = "quota"
	DbUser = "root"
	DbPass = "abcd"
)

// Syncer struct holds the common fields to be used by syncer
type Syncer struct {
	client kubernetes.Clientset
	mu     sync.Mutex // guards data
	data   map[string]string
}

// NewSyncer returns a new syncer object
func NewSyncer() *Syncer {
	kubeconfig := filepath.Join(
		os.Getenv("HOME"), ".kube", "config",
	)
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	return &Syncer{
		client: *client,
		data:   make(map[string]string),
	}
}

// TODO(rdas): give some thought to units or maybe take the input in databse as string??
func getCPUMilli(cpu int) string {
	return fmt.Sprintf("%s%s", strconv.Itoa(cpu), "m")
}

func getMemoryMI(memory int) string {
	return fmt.Sprintf("%s%s", strconv.Itoa(memory), "Mi")
}

// add more parameters to resource quota (e.g number of pods / services....)
func getResourceQuota(cpu, memory int) *v1.ResourceQuota {
	// Don's use "MustParse" it might panic at runtime , have some validation
	hard := v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(getCPUMilli(cpu)),
		v1.ResourceMemory: resource.MustParse(getMemoryMI(memory)),
	}

	return &v1.ResourceQuota{
		Spec: v1.ResourceQuotaSpec{
			Hard: hard,
		},
	}
}

func (s *Syncer) updateConfigMap() {
	_, err := s.client.CoreV1().ConfigMaps(*namespace).Get(*configmapName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		fmt.Printf("configmap: %s not found in namespace: %s, creating it ....", *configmapName, *namespace)

		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: *configmapName,
			},
		}
		cm, err := s.client.CoreV1().ConfigMaps(*namespace).Create(cm)
		if err != nil {
			fmt.Printf("failed to create configmap: %v\n", err)
		} else {
			fmt.Printf("successfully created config map: %v", cm)
		}
	}

}

func syncConfigMap(db *sql.DB) {
	// Fetch all the rowns from the table
	rows, err := db.Query("SELECT * FROM Resource")
	if err != nil {
		panic(err.Error())
	}

	resourceQuotas := make([]*v1.ResourceQuota, 0)
	var id, p, c, m int

	for rows.Next() {
		if err := rows.Scan(&id, &p, &c, &m); err != nil {
			panic(err.Error())
		}
		resourceQuotas = append(resourceQuotas, getResourceQuota(c, m))
	}

	for _, r := range resourceQuotas {
		fmt.Printf("%+v\n", r)
	}
}

func main() {
	dsn := DbUser + ":" + DbPass + "@" + DbHost + "/" + DbName + "?charset=utf8"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err.Error())
	}
	defer db.Close()
	syncConfigMap(db)

	s := NewSyncer()

	ticker := time.NewTicker(*syncInterval)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	for {
		select {
		case <-c:
			fmt.Println("logging off")
			os.Exit(0)
		case <-ticker.C:
			syncConfigMap(db)
			s.updateConfigMap()
		}
	}
}
