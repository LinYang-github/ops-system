import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 100, // 100个并发用户
  duration: '30s', // 持续30秒
};

export default function () {
  // 模拟心跳上报接口 (这是最高频的)
  const payload = JSON.stringify({
    port: 8081,
    info: { hostname: "load-test", ip: "1.2.3.4" },
    status: { cpu_usage: 50.0 }
  });
  
  const res = http.post('http://localhost:8080/api/worker/heartbeat', payload);
  
  check(res, {
    'status is 200': (r) => r.status === 200,
    'duration < 100ms': (r) => r.timings.duration < 100,
  });
  
  sleep(3); // 模拟心跳间隔
}