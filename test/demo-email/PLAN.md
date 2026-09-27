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
