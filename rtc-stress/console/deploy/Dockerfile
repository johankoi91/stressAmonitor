FROM hub.agoralab.co/rtn-ci/stress:hybrid_tools AS runtime-base
RUN getent group netdata >/dev/null || groupadd --system netdata
RUN id netdata >/dev/null 2>&1 || useradd --system --gid netdata --home-dir /opt/netdata --shell /usr/sbin/nologin netdata

FROM runtime-base AS netdata
COPY netdata-x86_64-v2.13.0.gz.run /tmp/netdata.gz.run
RUN echo "bb0be18228b49c9c816b87193a070ed93defdd8e366368899e39cb7d005ca6f7  /tmp/netdata.gz.run" | sha256sum -c - \
    && DO_NOT_TRACK=1 DISABLE_TELEMETRY=1 sh /tmp/netdata.gz.run --accept -- --dont-start-it --stable-channel --disable-telemetry --certificates bundled \
    && rm /tmp/netdata.gz.run \
    && rm -f /opt/netdata/var/lib/netdata/registry/netdata.public.unique.id

FROM runtime-base
COPY --from=netdata /opt/netdata /opt/netdata
COPY netdata.conf /opt/netdata/etc/netdata/netdata.conf
COPY sample_send_h264_pcm *.so /home/bbt/
COPY rtc-stress /opt/rtc-stress-console/rtc-stress
COPY rtc-sdk-worker /opt/rtc-stress-console/rtc-sdk-worker
COPY sdk/ /opt/rtc-stress-console/sdk/
COPY frontend/ /opt/rtc-stress-console/frontend/
COPY defaults/ /opt/rtc-stress-console/defaults/
RUN chmod +x /home/bbt/sample_send_h264_pcm /opt/rtc-stress-console/rtc-stress \
    && chmod +x /opt/rtc-stress-console/rtc-sdk-worker \
    && touch /opt/netdata/etc/netdata/.opt-out-from-anonymous-statistics
WORKDIR /home/bbt
ENV LD_LIBRARY_PATH=/home/bbt DO_NOT_TRACK=1 DISABLE_TELEMETRY=1
CMD ["/opt/rtc-stress-console/rtc-stress", "serve", "--listen", "0.0.0.0:8092", "--data-dir", "/var/lib/rtc-stress-console", "--frontend", "/opt/rtc-stress-console/frontend"]
