import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';

const TOTAL_WRITES = 10000
const TOTAL_LIST_OBJECTS_CALLS = 100000

const STORE_ID = __ENV.STORE_ID
if (!STORE_ID) {
    throw new Error("store must be defined")
}

const TUPLES_PER_WRITE = __ENV.TUPLES_PER_WRITE
if (!TUPLES_PER_WRITE) {
    throw new Error("tuples per write must be defined")
}

const counterTuplesWritten = new Counter('tuples_written');
const postgresMaxConnections = new Counter('postgres_max_connections');

export const options = {
    setupTimeout: '10m',
    teardownTimeout: '10m',
    scenarios: {
        check: {
            executor: 'constant-vus',
            duration: '2h',
            gracefulStop: '0s',
            exec: 'checkApi',
        },
        // list_objects_v0: {
        //     executor: 'constant-vus',
        //     duration: '2h',
        //     gracefulStop: '0s',
        //     exec: 'v0',
        //     startTime: '2h',  // duration + gracefulStop of the above
        // },
        // list_objects_v1: {
        //     executor: 'constant-vus',
        //     duration: '2h',
        //     gracefulStop: '0s',
        //     exec: 'v1',
        //     startTime: '4h',  // duration + gracefulStop of the above
        // },
    },
}

export function setup() {

    postgresMaxConnections.add(100)
    // 2. setup code
    let res = http.post(`http://localhost:${__ENV.PORT}/stores/${STORE_ID}/authorization-models`, JSON.stringify({
        "schema_version": "1.1",
        "type_definitions": [
            {
                "type": "user"
            },
            {
                "type": "document",
                "relations": {
                    "viewer": {
                        "this": {}
                    }
                },
                "metadata": {
                    "relations": {
                        "viewer": {
                            "directly_related_user_types": [
                                {
                                    "type": "user"
                                }
                            ]
                        }
                    }
                }
            }
        ]
    }), {
        headers: { 'Content-Type': 'application/json' },
    });

    let auth_model_id_v1 = res.json().authorization_model_id

    let resv0 = http.post(`http://localhost:${__ENV.PORT}/stores/${STORE_ID}/authorization-models`, JSON.stringify({
        "schema_version": "1.0",
        "type_definitions": [
            {
                "type": "user"
            },
            {
                "type": "document",
                "relations": {
                    "viewer": {
                        "this": {}
                    }
                }
            }
        ]
    }), {
        headers: { 'Content-Type': 'application/json' },
    });

    let auth_model_id_v0 = resv0.json().authorization_model_id

    // let track = 0
    // let requests = []
    // for (let i = 0; i < TOTAL_WRITES; i++) {
    //     let tuplesToWrite = []
    //
    //     for (let j = 0; j < TUPLES_PER_WRITE; j++) {
    //         tuplesToWrite.push({
    //             user: `user:${track}`,
    //             object: `document:${track}`,
    //             relation: `viewer`
    //         })
    //         counterTuplesWritten.add(1)
    //         track++
    //     }
    //     requests.push(['POST', `http://localhost:${__ENV.PORT}/stores/${STORE_ID}/write`, JSON.stringify({
    //         "writes": {
    //             "tuple_keys": tuplesToWrite
    //         }
    //     }), null])
    // }
    //
    // http.batch(requests);

    return { authorization_model_id_v1: auth_model_id_v1, authorization_model_id_v0: auth_model_id_v0};
}

export function v0 (setupData) {
    console.log(new Date())
    console.log("begin v0")
    for (let i = 0; i < TOTAL_LIST_OBJECTS_CALLS; i++) {
        let data = {
            relation: "viewer",
            type: "document",
            user: `user:${i}`,
            authorization_model_id: setupData.authorization_model_id_v0
        };
        let res = http.post(`http://localhost:${__ENV.PORT}/stores/${STORE_ID}/list-objects`, JSON.stringify(data), {
            headers: { 'Content-Type': 'application/json' },
        });
        check(res, { 'complete results': (r) => r.body && JSON.parse(r.body)["objects"] && JSON.parse(r.body)["objects"].length === 1 });
        check(res, { 'success call': (r) => r.status === 200 });

    }

    console.log(new Date())
    console.log("done v0")
}

export function v1 (setupData) {
    console.log(new Date())
    console.log("begin v1")
    for (let i = 0; i < TOTAL_LIST_OBJECTS_CALLS; i++) {
        let data = {
            relation: "viewer",
            type: "document",
            user: `user:${i}`,
            authorization_model_id: setupData.authorization_model_id_v1
        };
        let res = http.post(`http://localhost:${__ENV.PORT}/stores/${STORE_ID}/list-objects`, JSON.stringify(data), {
            headers: { 'Content-Type': 'application/json' },
        });
        check(res, { 'complete results': (r) => r.body && JSON.parse(r.body)["objects"] && JSON.parse(r.body)["objects"].length === 1 });
        check(res, { 'success call': (r) => r.status === 200 });
    }

    console.log(new Date())
    console.log("done v1")
}

export function checkApi (setupData) {
    console.log(new Date())
    console.log("begin check")
    for (let i = 0; i < TOTAL_LIST_OBJECTS_CALLS; i++) {
        let data = {
            tuple_key: {
                relation: "viewer",
                object: "document:2",
                user: `user:${i}`
            }
        };
        let res = http.post(`http://localhost:${__ENV.PORT}/stores/${STORE_ID}/check`, JSON.stringify(data), {
            headers: { 'Content-Type': 'application/json' },
        });
        check(res, { 'complete results': (r) => r.body && JSON.parse(r.body)["allowed"]});
        check(res, { 'success call': (r) => r.status === 200 });
    }
    console.log(new Date())
    console.log("end check")
}

export function teardown() {
    // 4. teardown code
    // let track = 0
    // let requests = []
    // for (let i = 0; i < TOTAL_WRITES; i++) {
    //     let tuplesToWrite = []
    //
    //     for (let j = 0; j < TUPLES_PER_WRITE; j++) {
    //         tuplesToWrite.push({
    //             user: `user:${track}`,
    //             object: `document:${track}`,
    //             relation: `viewer`
    //         })
    //         track++
    //     }
    //     requests.push(['POST', `http://localhost:${__ENV.PORT}/stores/${STORE_ID}/write`, JSON.stringify({
    //         "deletes": {
    //             "tuple_keys": tuplesToWrite
    //         }
    //     }), null])
    // }
    //
    // http.batch(requests);
}

export function handleSummary(data) {
    delete data.metrics['http_req_tls_handshaking'];

    return {
        stdout: textSummary(data, { indent: '→', enableColors: true }),
    };
}