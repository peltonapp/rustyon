// SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
// SPDX-License-Identifier: BSD-3-Clause

//! Typed fixture used for the round trip tests between ron-rs and rustyon.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Meters(pub f64);

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Pair(pub i32, pub String);

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Marker;

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Inner {
    pub x: i32,
    pub y: i32,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub enum Shape {
    Empty,
    Radius(f32),
    Rect(u32, u32),
    Poly { sides: u8, name: String },
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Fixture {
    pub name: String,
    pub count: u64,
    pub neg: i64,
    pub big: i128,
    pub ubig: u128,
    pub ratio: f32,
    pub exact: f64,
    pub flag: bool,
    pub letter: char,
    pub text: String,
    #[serde(with = "serde_bytes")]
    pub blob: Vec<u8>,
    pub meters: Meters,
    pub pair: Pair,
    pub marker: Marker,
    pub inner: Inner,
    pub tuple: (i32, String, bool),
    pub unit: (),
    pub shapes: Vec<Shape>,
    pub some: Option<i32>,
    pub none: Option<i32>,
    pub nested_some: Option<Option<i32>>,
    pub nested_none: Option<Option<i32>>,
    pub nested_some_none: Option<Option<i32>>,
    pub list: Vec<i32>,
    pub empty: Vec<i32>,
    pub map: BTreeMap<String, i32>,
    pub int_map: BTreeMap<u8, Vec<String>>,
}

pub fn expected() -> Fixture {
    let mut map = BTreeMap::new();
    map.insert("one".to_string(), 1);
    map.insert("two".to_string(), 2);
    let mut int_map = BTreeMap::new();
    int_map.insert(1u8, vec!["a".to_string(), "b".to_string()]);
    int_map.insert(2u8, vec![]);
    Fixture {
        name: "fixture".to_string(),
        count: u64::MAX,
        neg: i64::MIN,
        big: i128::MIN,
        ubig: u128::MAX,
        ratio: 0.1,
        exact: -1.5e300,
        flag: true,
        letter: '\u{1F600}',
        text: "quote \" backslash \\ newline \n tab \t nul \0 unicode \u{e9}\u{4e2d}\u{1F600} bell \u{7} del \u{7f}"
            .to_string(),
        blob: vec![0, 1, 2, 0x7f, 0x80, 0xff],
        meters: Meters(2.5),
        pair: Pair(7, "seven".to_string()),
        marker: Marker,
        inner: Inner { x: 1, y: -2 },
        tuple: (3, "three".to_string(), false),
        unit: (),
        shapes: vec![
            Shape::Empty,
            Shape::Radius(1.5),
            Shape::Rect(3, 4),
            Shape::Poly {
                sides: 5,
                name: "penta".to_string(),
            },
        ],
        some: Some(5),
        none: None,
        nested_some: Some(Some(6)),
        nested_none: None,
        nested_some_none: Some(None),
        list: vec![1, 2, 3],
        empty: vec![],
        map,
        int_map,
    }
}

/// print writes the fixture compact, a separator line, then pretty.
pub fn print() {
    let f = expected();
    let compact = ron::to_string(&f).expect("serialize compact");
    let pretty = ron::ser::to_string_pretty(&f, ron::ser::PrettyConfig::default())
        .expect("serialize pretty");
    println!("{compact}");
    println!("---");
    println!("{pretty}");
}

/// check deserializes input and returns 0 when it equals the expected fixture.
pub fn check(input: &str) -> i32 {
    match ron::from_str::<Fixture>(input) {
        Ok(got) => {
            let want = expected();
            if got == want {
                0
            } else {
                eprintln!("fixture mismatch\n got: {got:?}\nwant: {want:?}");
                1
            }
        }
        Err(e) => {
            eprintln!("deserialize error: {e}");
            1
        }
    }
}
