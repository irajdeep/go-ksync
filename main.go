package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
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
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// (TODO): Remove database configurations from here and read them from a secret or something
var (
	configmapName = flag.String("config-map-name", "namespace-controller", "pass the configmap name to be watched")
	syncInterval  = flag.Duration("sync-duration", 10*time.Second, "sync interval for syncing configmap")
	namespace     = flag.String("namespace", "namespace-controller", "namespace the syncer cares to sync the cm in")
	dbHost        = flag.String("db-host", "tcp(127.0.0.1:3306)", "database host to connect to")
	dbName        = flag.String("db-name", "quota", "database to connect to")
	dbUser        = flag.String("db-user", "root", "database user")
	dbPass        = flag.String("db-pass", "abcd", "database passord")
)

// Syncer struct holds the common fields to be used by syncer
type Syncer struct {
	client kubernetes.Clientset
	mu     sync.Mutex // guards data
	data   map[string]*v1.ResourceQuota
	db     *sql.DB
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

	dsn := *dbUser + ":" + *dbPass + "@" + *dbHost + "/" + *dbName + "?charset=utf8"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err.Error())
	}

	return &Syncer{
		client: *client,
		data:   make(map[string]*v1.ResourceQuota),
		db:     db,
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

func (s *Syncer) getDataFromCache() map[string]string {
	m := make(map[string]string)

	s.mu.Lock()
	defer s.mu.Unlock()

	for k, v := range s.data {
		j, err := json.Marshal(v)
		if err != nil {
			panic(err.Error())
		}
		m[k] = string(j)
	}
	return m
}

func (s *Syncer) updateConfigMap() {
	cm, err := s.client.CoreV1().ConfigMaps(*namespace).Get(*configmapName, metav1.GetOptions{})

	if errors.IsNotFound(err) {
		log.Printf("configmap: %s not found in namespace: %s, creating it ....\n", *configmapName, *namespace)

		cm = &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: *configmapName,
			},
		}
		cm, err = s.client.CoreV1().ConfigMaps(*namespace).Create(cm)
		if err != nil {
			log.Printf("failed to create configmap: %v\n", err)
		} else {
			log.Printf("successfully created config map: %v", cm)
		}
	}

	// Update the configmap for now
	// (TODO) : Perform a patch instead of update also we might not want
	// to change the entire datasection of the configmap and just the diff
	cm.Data = s.getDataFromCache()

	cm, err = s.client.CoreV1().ConfigMaps(*namespace).Update(cm)
	if err != nil {
		panic(err.Error())
	}
	log.Printf("sucessfully updated configmap in namespace: %s, cm: %s", *namespace, *configmapName)
}

func (s *Syncer) syncCache() {
	// Fetch all the rowns from the table
	rows, err := s.db.Query("SELECT * FROM Resource")
	if err != nil {
		panic(err.Error())
	}

	var (
		id, pID, c, m int
		pName         string
	)

	s.mu.Lock()
	defer s.mu.Unlock()

	for rows.Next() {
		if err := rows.Scan(&id, &pID, &pName, &c, &m); err != nil {
			panic(err.Error())
		}
		s.data[pName] = getResourceQuota(c, m)
	}
}

func main() {
	flag.Parse()

	s := NewSyncer()
	defer s.db.Close()

	ticker := time.NewTicker(*syncInterval)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	for {
		select {
		case <-c:
			log.Println("logging off")
			os.Exit(0)
		case <-ticker.C:
			s.syncCache()
			s.updateConfigMap()
		}
	}
}
