ARG PROBE_BASE_IMAGE=mcr.microsoft.com/playwright:v1.56.1-noble
FROM ${PROBE_BASE_IMAGE}
ARG NPM_REGISTRY=https://registry.npmmirror.com
WORKDIR /opt/rtc-probe
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright \
    PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 \
    PROBE_OUTPUT=/results \
    TZ=Asia/Shanghai
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --ignore-scripts --registry="${NPM_REGISTRY}" \
    && npm cache clean --force
COPY run.mjs server.mjs sdk-adapter.js ./
COPY browser/ ./browser/
COPY sdk/ ./sdk/
COPY fixtures/ ./fixtures/
RUN mkdir -p /results
ENTRYPOINT ["node", "run.mjs"]
