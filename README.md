This is a proxy designed to accept ArgoCD triggers and use them to create released releases and deployments in an Octopus instance.

The proxy is deployed with the following resoutces:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: octoargosync
  namespace: argocd
  labels:
    app: octoargosync
spec:
  selector:
    matchLabels:
      app: octoargosync
  replicas: 1
  strategy:
    type: RollingUpdate
  template:
    metadata:
      labels:
        app: octoargosync
    spec:
      containers:
        - name: octoargosync
          image: octopussamples/octoargosync
          imagePullPolicy: Always
          ports:
            - containerPort: 8080
          env:
            - name: ARGOCD_SERVER
              value: argocd-server.argocd.svc.cluster.local:443
            - name: ARGOCD_TOKEN
              valueFrom:
                secretKeyRef:
                  name: octoargosync-secret
                  key: argotoken
            - name: OCTOPUS_SERVER
              value: http://octopus:8080
            - name: OCTOPUS_SPACE_ID
              value: Spaces-4
            - name: OCTOPUS_API_KEY
              value: API-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
---
apiVersion: v1
kind: Service
metadata:
  name: octoargosync
  namespace: argocd
spec:
  selector:
    app: octoargosync
  ports:
    - protocol: TCP
      port: 80
      targetPort: 8080

```

The proxy scans projects in the configured space for known variables that map an Octopus project to an ArgoCD project. 
In the variable names below, `namespace` must be replaced with the namespace hosting an ArgoCD Application, and 
`applicationname` must be replaced with the Application name:

* `Metadata.ArgoCD.Application[namespace/applicationname].Environment` - Set the value to the name of an Octopus environment. This links the ArgoCD Application to an Octopus environment.
* `Metadata.ArgoCD.Application[namespace/applicationname].ImageForReleaseVersion` - Set the value to a Docker image included in an ArgoCD Application. The tag of the Docker image will be used when creating the Octopus release version.
* `Metadata.ArgoCD.Application[namespace/applicationname].ImageForPackageVersion[actionname:packagename]` - Set the value to a Docker image included in an ArgoCD Application. This sets the value of the package defined in the action called `actioname` with the name `packagename` to the version of the linked image tag. 

Triggers are configured in the `argocd-notifications-cm` ConfigMap:
```
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-notifications-cm
  namespace: argocd
data:
  trigger.on-deployed: |
    - description: Application is synced and healthy. Triggered once per commit.
      send:
      - octopus-deployment-status
      when: app.status.operationState.phase in ['Succeeded'] and app.status.health.status == 'Healthy'
  template.octopus-deployment-status: |
    webhook:
      octopus:
        method: POST
        path: /api/octopusrelease
        body: |
          {
            "Application": "{{.app.metadata.name}}",
            "Namespace": "{{.app.metadata.namespace}}",
            "Project": "{{.app.spec.project}}",
            "State": "Success",
            "CommitSha": "{{.app.status.operationState.operation.sync.revision}}",
            "TargetRevision": "{{.app.spec.source.targetRevision}}",
            "TargetUrl": "{{.context.argocdUrl}}/applications/{{.app.metadata.name}}"
          }
  service.webhook.octopus: |
    url: http://octoargosync.argocd.svc.cluster.local
    headers:
    - name: Content-type
      value: application/json
```
