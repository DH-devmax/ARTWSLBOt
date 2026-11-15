FROM wsl-test:chili-settings-20261102
RUN rm -rf /src/internal /src/cmd /src/tools /src/docs /src/web
COPY . .
