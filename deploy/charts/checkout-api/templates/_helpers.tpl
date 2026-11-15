{{/*
Chart name, fully qualified name, labels, and shared pod template.
*/}}

{{- define "checkout-api.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "checkout-api.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{- define "checkout-api.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "checkout-api.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: meridian
meridian.io/team: platform
{{- end }}

{{- define "checkout-api.selectorLabels" -}}
app.kubernetes.io/name: {{ include "checkout-api.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "checkout-api.redisLabels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "checkout-api.fullname" . }}-redis
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: meridian
meridian.io/team: platform
app.kubernetes.io/component: redis
{{- end }}

{{- define "checkout-api.redisSelectorLabels" -}}
app.kubernetes.io/name: {{ include "checkout-api.fullname" . }}-redis
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: redis
{{- end }}

{{- define "checkout-api.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "checkout-api.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "checkout-api.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) }}
{{- end }}

{{/*
The pod template is shared by the Deployment and the Rollout so hardening and
probe behavior cannot drift between environments.
*/}}
{{- define "checkout-api.podTemplate" -}}
metadata:
  labels:
    {{- include "checkout-api.selectorLabels" . | nindent 4 }}
    {{- with .Values.podLabels }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
  {{- with .Values.podAnnotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  serviceAccountName: {{ include "checkout-api.serviceAccountName" . }}
  automountServiceAccountToken: {{ .Values.serviceAccount.automountServiceAccountToken }}
  terminationGracePeriodSeconds: {{ .Values.terminationGracePeriodSeconds }}
  securityContext:
    {{- toYaml .Values.podSecurityContext | nindent 4 }}
  {{- with .Values.imagePullSecrets }}
  imagePullSecrets:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  containers:
    - name: api
      image: {{ include "checkout-api.image" . | quote }}
      imagePullPolicy: {{ .Values.image.pullPolicy }}
      securityContext:
        {{- toYaml .Values.securityContext | nindent 8 }}
      ports:
        - name: http
          containerPort: {{ .Values.containerPort }}
          protocol: TCP
      env:
        - name: HTTP_ADDR
          value: ":{{ .Values.containerPort }}"
        - name: LOG_LEVEL
          value: {{ .Values.config.logLevel | quote }}
        - name: SHUTDOWN_TIMEOUT
          value: {{ .Values.config.shutdownTimeout | quote }}
        - name: IDEMPOTENCY_TTL
          value: {{ .Values.config.idempotencyTTL | quote }}
        - name: MAX_BODY_BYTES
          value: {{ .Values.config.maxBodyBytes | int64 | quote }}
        - name: FAULT_LATENCY_MS
          value: {{ .Values.config.faultLatencyMs | int64 | quote }}
        {{- if .Values.redis.enabled }}
        - name: REDIS_ADDR
          value: {{ printf "%s-redis:%d" (include "checkout-api.fullname" .) (int .Values.redis.port) | quote }}
        {{- end }}
        {{- with .Values.extraEnv }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      readinessProbe:
        httpGet:
          path: /readyz
          port: http
        periodSeconds: {{ .Values.probes.readiness.periodSeconds }}
        timeoutSeconds: {{ .Values.probes.readiness.timeoutSeconds }}
        failureThreshold: {{ .Values.probes.readiness.failureThreshold }}
      livenessProbe:
        httpGet:
          path: /healthz
          port: http
        periodSeconds: {{ .Values.probes.liveness.periodSeconds }}
        timeoutSeconds: {{ .Values.probes.liveness.timeoutSeconds }}
        failureThreshold: {{ .Values.probes.liveness.failureThreshold }}
      startupProbe:
        httpGet:
          path: /healthz
          port: http
        periodSeconds: {{ .Values.probes.startup.periodSeconds }}
        timeoutSeconds: {{ .Values.probes.startup.timeoutSeconds }}
        failureThreshold: {{ .Values.probes.startup.failureThreshold }}
      lifecycle:
        preStop:
          httpGet:
            path: /drainz
            port: http
      resources:
        {{- toYaml .Values.resources | nindent 8 }}
  {{- with .Values.nodeSelector }}
  nodeSelector:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .Values.affinity }}
  affinity:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .Values.tolerations }}
  tolerations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with .Values.topologySpreadConstraints }}
  topologySpreadConstraints:
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- end }}
