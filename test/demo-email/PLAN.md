# Demo Agenda: Email Operator with Decision Maker

## Overview
This demo walks through deploying a Helm chart that installs a Kubebuilder‑generated email operator, a validation webhook, and an agent trigger. We’ll see how the operator sends emails labeled `readytosend`, how the webhook rejects sensitive messages, and how a sub‑agent summarizes and updates a ConfigMap before the final send.

## Prerequisites
- Helm 3 installed and a cluster ready.
- `kubectl` configured to talk to the cluster.
- `harikube` CLI available.
- `make` for building the operator.

## Demo Agenda
1. **Helm Install** – `helm install email-operator .`
2. **Kubebuilder Init** – `kubebuilder init --domain example.com`
3. **Generate Operator** – `make generate` (creates CRDs, controller, webhook)
4. **Deploy Validation Webhook** – uses Decision Maker to reject sensitive emails.
5. **Create Agent Trigger** – `kubectl apply -f pi-trigger.yaml` (wakes on email creation)
6. **Run Sub‑Agent** – summarises email, hibernates, then calls Decision Maker.
7. **Update Transaction** – `kubectl harikube transaction create -f email+configmap.yaml` updates ConfigMap and email status.
8. **Watch Outgoing Stream** – `kubectl logs -f <pod>` to see the decision and final send.

## Terminal Split
- **Terminal 1**: Helm install, operator logs, webhook decisions.
- **Terminal 2**: `kubectl harikube transaction create` and ConfigMap watch.
- **Terminal 3**: `kubectl logs -f` to observe the decision stream.

## Talking Points
- Explain the causal chain: webhook → operator → agent trigger → sub‑agent → Decision Maker → transaction update → email send.
- Highlight how the `readytosend` label is applied only after approval.
- Discuss the API‑extension transaction that atomically updates the ConfigMap and email status.

## Expected Outcomes
- Operator starts and registers webhook.
- Sensitive email is rejected by Decision Maker.
- Non‑sensitive email is approved, summarized, and sent.
- ConfigMap reflects the decision and email status.

## Fallback Paths
- If webhook fails, operator logs error and retries.
- If sub‑agent hangs, the session hibernates and can be resumed.
- If Decision Maker denies, email remains unsent and status is updated.

## Wrap‑Up
Summarise the flow, emphasize the decoupling of decision logic, and invite questions.

---

"I'm preparing for a demo—I'll be demoing harikube, along with how I taught AI a few cloud-native tricks:

    helm install

    kubebuilder init

    pi "generate an email operator based on kubebuilder which sends emails labeled with readytosend. implement a validation webhook and call api-extension decision maker to detect and decline emails with sensitive information"

    pi "generate an agent trigger custom resource based on this CRD to wake up an agent on email creation with the prompt execute email summarizing in sub-agent, then hibernate the session. Once the sub agent has finished its job, call decision maker to decide was it able to make the decision, and update config map with summary and email status and readytosend label in api-extension transaction based on this CRD"

    kubectl harikube transaction create -f email+configmap.yaml

    The validation webhook calls the decision maker, which validates the email
    (the operator sends out the email)

    On the other terminal, I'm watching the zero-overhead outgoing stream to see what decision was made"



Ez elegáns. A jóváhagyás itt nem egy külön rendszer, hanem egy újabb watch-esemény: az agent leteszi a javaslatot, feliratkozik rá, és akkor ébred fel, amikor egy ember átírja. Nem kell hozzá workflow engine, jóváhagyó UI vagy callback. Ezt a demóban meg is érdemes mutatni, mert ütős. egy masik agent hagyja jova :D
 "Egyszerű, lineáris taskok" otletet adtal, a defaulting webhookban layaval fellabelezzuk, es akkor trigger szinten tudsz labelszurest - Az agent felülbírálhatja. A címke csak javaslat, a végső döntés az agenté marad. Így nem kell kívülről eldönteni, mi történjen belül, amit te is kifogásoltál.


# Federation cluster

helm install harikube
kubectl create secret generic pi-agent-config --from-file=~/.pi/agent/auth.json --from-file=~/.pi/agent/models-store.json
kubectl create secret generic pi-agent-worker --from-file=kubeconfig=${WORKER_KUBECONFIG}
cat | kubectl -f <<<EOF 
apiVersion: triggers.harikube.info/v1
kind: PiTrigger
metadata:
  name: federation-agent
spec:
  resource:
    apiVersion: v1
    kind: ConfigMap
  eventTypes:
    - ADDED
  labelSelectors:
    - operation=review
    - review=pending
  lockDuration: 2m
  watcherKubeconfigSecret: worker-cluster
  agent:
    image: docker.io/mhmxs/pi-agent-empty:latest
    configSecretRef:
      name: pi-agent-config
    workingDir: /workspace
    timeout: 1m
    serviceAccountName: pi-agent-worker
    imagePullPolicy: IfNotPresent
    env:
      - name: PI_ENVIRONMENT
        value: production
    resources:
      requests:
        cpu: 100m
        memory: 256Mi
      limits:
        cpu: 500m
        memory: 512Mi
    backoffLimit: 3
    activeDeadlineSeconds: 1800
    ttlSecondsAfterFinished: 600
    prompt: |
      You are a decision reviwer, review the decision, ensure the decision doesn't risk the pandas, validate decision via `decision_maker`.  Decision details:
EOF

# Worker cluster

helm install harikube
kubectl create secret generic pi-agent-config --from-file=~/.pi/agent/auth.json --from-file=~/.pi/agent/models-store.json
cat | kubectl -f <<<EOF 
apiVersion: triggers.harikube.info/v1
kind: PiTrigger
metadata:
  name: worker-agent
spec:
  resource:
    apiVersion: v1
    kind: ConfigMap
  eventTypes:
    - ADDED
  labelSelectors:
    - operation=building
  lockDuration: 11m
  agent:
    image: docker.io/mhmxs/pi-agent-empty:latest
    configSecretRef:
      name: pi-agent-config
    workingDir: /workspace
    timeout: 10m
    serviceAccountName: pi-agent-worker
    imagePullPolicy: IfNotPresent
    env:
      - name: PI_ENVIRONMENT
        value: production
    resources:
      requests:
        cpu: 100m
        memory: 256Mi
      limits:
        cpu: 500m
        memory: 512Mi
    backoffLimit: 3
    activeDeadlineSeconds: 1800
    ttlSecondsAfterFinished: 600
    prompt: |
      Before each mutate step `create_pitrigger` with the modifications below.           
      ```
      eventTypes:
      - MODIFIED
      labelSelectors:
      - operation=review
      - review
      ```
      If you are triggered with a payload labeled with: review=declined, delete ConfigMap, call `exit_pi` with non zero code and meaningful error message.
      Otherwise, delete ConfigMap do the next step, never wait for user input, ask `decisioni_maker` to make decisions.
EOF

cat | kubectl -f <<<EOF 
apiVersion: triggers.harikube.info/v1
kind: PiTrigger
metadata:
  name: doorkeeper-agent
spec:
  resource:
    apiVersion: v1
    kind: ConfigMap
  eventTypes:
    - ADDED
  labelSelectors:
    - operation=doorkeeping
  lockDuration: 3m
  agent:
    image: docker.io/mhmxs/pi-agent-empty:latest
    configSecretRef:
      name: pi-agent-config
    workingDir: /workspace
    timeout: 2m
    serviceAccountName: pi-agent-worker
    imagePullPolicy: IfNotPresent
    env:
      - name: PI_ENVIRONMENT
        value: production
    resources:
      requests:
        cpu: 100m
        memory: 256Mi
      limits:
        cpu: 500m
        memory: 512Mi
    backoffLimit: 3
    activeDeadlineSeconds: 1800
    ttlSecondsAfterFinished: 600
    prompt: |
      You are a doorkeeper, who needs to allow everybody in. If someone asks for resource permissions, give full permissions to all resources via `exec_kubectl` at the role of: pi-agent-worker.
      Once the change is available, delete the ConfigMap triggered the event.
EOF

cat | kubectl -f <<<EOF 
apiVersion: v1
kind: ConfigMap
metadata:
  name: wordpress-demodata
  labels:
    operation=building
data:
  01-step: Design a custom resource for todo application, and make it available on cluster via `exec_kubectl`
  02-step: Validate todo custom resource is available at the cluster via `kubernetes-service-discovery-runtime` - fix if not available
  03-step: |
    Use `kubernetes-service-discovery-runtime` skill to fetch available cluster services, and create a ConfigMap with labels: [operation=doorkeeping, agent=worker-agent], and kind.api-group/version format list the custom resource definitions.
    `create_pitrigger` with the modifications below and call `exit_pi`.           
      eventTypes:
      - DELETED
      labelSelectors:
      - operation=doorkeeping
      - agent=worker-agent
  04-step: Use `kubernetes-service-discovery-runtime` skill to fetch available cluster services, and look for todo, and create a sample todo item
  05-step: Validate todo item is exists
EOF