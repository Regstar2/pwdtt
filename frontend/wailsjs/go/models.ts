export namespace backend {
  export class ConnectParams {
    peerAddr: string;
    password: string;
    hashes: string[];
    deviceId?: string;
    workers?: number;
    captchaMode?: string;
    obfsMode?: string;
    fingerprint?: string;
    profileName?: string;
    hashMode?: string;
    hashAutoCheck?: boolean;
    hashAutoReplace?: boolean;
    operationId?: string;

    static createFrom(source: any = {}) { return new ConnectParams(source); }
    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source);
      this.peerAddr = source["peerAddr"];
      this.password = source["password"];
      this.hashes = source["hashes"];
      this.deviceId = source["deviceId"];
      this.workers = source["workers"];
      this.captchaMode = source["captchaMode"];
      this.obfsMode = source["obfsMode"];
      this.fingerprint = source["fingerprint"];
      this.profileName = source["profileName"];
      this.hashMode = source["hashMode"];
      this.hashAutoCheck = source["hashAutoCheck"];
      this.hashAutoReplace = source["hashAutoReplace"];
      this.operationId = source["operationId"];
    }
  }

  export class IPInfo {
    query: string;
    country: string;
    status: string;
    latency: number;
    static createFrom(source: any = {}) { return new IPInfo(source); }
    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source);
      this.query = source["query"];
      this.country = source["country"];
      this.status = source["status"];
      this.latency = source["latency"];
    }
  }

  export class LogEntry {
    level: string;
    message: string;
    time: string;
    count: number;
    static createFrom(source: any = {}) { return new LogEntry(source); }
    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source);
      this.level = source["level"];
      this.message = source["message"];
      this.time = source["time"];
      this.count = source["count"];
    }
  }

  export class VKHashPolicy {
    mode: string;
    autoCheck: boolean;
    autoReplace: boolean;
    static createFrom(source: any = {}) { return new VKHashPolicy(source); }
    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source);
      this.mode = source["mode"];
      this.autoCheck = source["autoCheck"];
      this.autoReplace = source["autoReplace"];
    }
  }

  export class ProfileData {
    peer: string;
    password: string;
    hashes: string[];
    listen: string;
    turn: string;
    port: string;
    device_id: string;
    hash_policy?: VKHashPolicy;
    static createFrom(source: any = {}) { return new ProfileData(source); }
    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source);
      this.peer = source["peer"];
      this.password = source["password"];
      this.hashes = source["hashes"];
      this.listen = source["listen"];
      this.turn = source["turn"];
      this.port = source["port"];
      this.device_id = source["device_id"];
      this.hash_policy = source["hash_policy"] ? new VKHashPolicy(source["hash_policy"]) : undefined;
    }
  }

  export class UpdateInfo {
    available: boolean;
    version: string;
    url: string;
    body: string;
    static createFrom(source: any = {}) { return new UpdateInfo(source); }
    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source);
      this.available = source["available"];
      this.version = source["version"];
      this.url = source["url"];
      this.body = source["body"];
    }
  }

  export class VKHashCheck {
    status: string;
    checkedAt: number;
    errorType?: string;
    message?: string;
    latencyMs?: number;
    static createFrom(source: any = {}) { return new VKHashCheck(source); }
    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source);
      this.status = source["status"];
      this.checkedAt = source["checkedAt"];
      this.errorType = source["errorType"];
      this.message = source["message"];
      this.latencyMs = source["latencyMs"];
    }
  }

  export class VKHashEntry {
    id: string;
    hash: string;
    source: string;
    inPool: boolean;
    createdAt: number;
    checks?: Record<string, VKHashCheck>;
    usedBy?: string[];
    static createFrom(source: any = {}) { return new VKHashEntry(source); }
    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source);
      this.id = source["id"];
      this.hash = source["hash"];
      this.source = source["source"];
      this.inPool = source["inPool"];
      this.createdAt = source["createdAt"];
      this.checks = source["checks"];
      this.usedBy = source["usedBy"];
    }
  }

  export class VKHashCheckResult {
    hashId: string;
    hash: string;
    profileName: string;
    status: string;
    checkedAt: number;
    errorType?: string;
    message?: string;
    latencyMs?: number;
    static createFrom(source: any = {}) { return new VKHashCheckResult(source); }
    constructor(source: any = {}) {
      if ('string' === typeof source) source = JSON.parse(source);
      this.hashId = source["hashId"];
      this.hash = source["hash"];
      this.profileName = source["profileName"];
      this.status = source["status"];
      this.checkedAt = source["checkedAt"];
      this.errorType = source["errorType"];
      this.message = source["message"];
      this.latencyMs = source["latencyMs"];
    }
  }
}
