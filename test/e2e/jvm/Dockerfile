FROM docker.io/library/eclipse-temurin@sha256:0a3f329821dcadf54b2a3033a15e2e9f007069a9db43d7f51897e6b80e726b07
WORKDIR /app
COPY Load.java /app/Load.java
RUN javac Load.java
USER 65532:65532
ENTRYPOINT ["java", "-Xms128m", "-Xmx256m", "Load"]
